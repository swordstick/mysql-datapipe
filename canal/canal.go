package canal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"dump"

	"github.com/juju/errors"
	"github.com/ngaut/log"
	"github.com/siddontang/go-mysql/client"
	"github.com/siddontang/go-mysql/mysql"
	"github.com/siddontang/go-mysql/replication"
	"github.com/siddontang/go-mysql/schema"
	"github.com/siddontang/go/sync2"
)

var errCanalClosed = errors.New("canal was closed")
var MysqlTimeout string = "?timeout=10000ms&writeTimeout=10000ms&readTimeout=10000ms"

const ThreadforDump = 20

var MasterSave *masterInfo

// Canal can sync your MySQL data into everywhere, like Elasticsearch, Redis, etc...
// MySQL must open row format for binlog
type Canal struct {
	m sync.Mutex

	cfg *Config

	master     *masterInfo
	dumper     *dump.Dumper
	dumpDoneCh chan struct{}
	syncer     *replication.BinlogSyncer
	dumppool   *Pool

	rsLock     sync.Mutex
	rsHandlers []RowsEventHandler

	quHandlers []QueryEventHandler

	connLock sync.Mutex
	conn     *client.Conn

	wg sync.WaitGroup

	tableLock sync.Mutex
	tables    map[string]*schema.Table

	quit   chan struct{}
	closed sync2.AtomicBool
}

func NewCanal(cfg *Config) (*Canal, error) {
	c := new(Canal)
	c.cfg = cfg
	c.closed.Set(false)
	c.quit = make(chan struct{})

	os.MkdirAll(cfg.DataDir, 0755)

	// 指定log的级别和日志文件

	log.SetLevelByString(cfg.LogLevel)
	log.SetOutputByName(path.Join(cfg.LogDir, cfg.LogFile))

	c.dumpDoneCh = make(chan struct{})
	c.rsHandlers = make([]RowsEventHandler, 0, 4)
	// add dumppool quHandlers
	c.dumppool = NewPool(ThreadforDump)
	//log.Infof("dumpThread is %d", cfg.dumpThreads) // debug
	c.quHandlers = make([]QueryEventHandler, 0, 4)
	c.tables = make(map[string]*schema.Table)

	var err error
	if c.master, err = loadMasterInfo(c.masterInfoPath()); err != nil {
		return nil, errors.Trace(err)
	} else if len(c.master.Addr) != 0 && c.master.Addr != c.cfg.Addr {
		log.Infof("MySQL addr %s in old master.info, but new %s, reset", c.master.Addr, c.cfg.Addr)
		// may use another MySQL, reset
		c.master = &masterInfo{}
	}

	MasterSave = c.master

	c.master.Addr = c.cfg.Addr

	if err := c.prepareDumper(); err != nil {
		return nil, errors.Trace(err)
	}

	if err = c.prepareSyncer(); err != nil {
		return nil, errors.Trace(err)
	}

	if err := c.checkBinlogRowFormat(); err != nil {
		return nil, errors.Trace(err)
	}
	return c, nil
}

func (c *Canal) SaveConfig() {
	c.master.Save(true)
}

func (c *Canal) prepareDumper() error {
	var err error
	dumpPath := c.cfg.Dump.ExecutionPath
	if len(dumpPath) == 0 {
		// ignore mysqldump, use binlog only
		return nil
	}

	if c.dumper, err = dump.NewDumper(dumpPath,
		c.cfg.Addr, c.cfg.User, c.cfg.Password); err != nil {
		return errors.Trace(err)
	}

	if c.dumper == nil {
		//no mysqldump, use binlog only
		return nil
	}

	tables := c.cfg.Dump.Tables
	tableDB := c.cfg.Dump.TableDB

	if len(tableDB) == 0 {
		panic("tableDB can not be Empty !!!")
	} else if len(tableDB) != 0 && len(tables) == 0 {
		c.dumper.TableDB = tableDB
	} else {
		for _, sub := range tables {
			c.dumper.AddTables(tableDB, sub)
		}
	}

	for _, sub := range c.cfg.Dump.IgnoreTables {
		c.AddDumpIgnoreTables(c.dumper.TableDB, sub)
	}

	if c.cfg.Dump.DiscardErr {
		c.dumper.SetErrOut(ioutil.Discard)
	} else {
		c.dumper.SetErrOut(os.Stderr)
	}

	return nil
}

func (c *Canal) Start() chan error {
	c.wg.Add(1)

	quit := make(chan error, 1)
	go c.run(quit)

	return quit
}

func (c *Canal) run(quit chan error) error {
	defer c.wg.Done()
	defer close(quit)

	if err := c.tryDump(); err != nil {
		log.Errorf("canal dump mysql err: %v", err)
		quit <- fmt.Errorf("canal dump mysql err: %v", err)
		return errors.Trace(err)
	}

	close(c.dumpDoneCh)

	if err := c.startSyncBinlog(); err != nil {
		if !c.isClosed() {
			log.Errorf("canal start sync binlog err: %v", err)
			quit <- fmt.Errorf("canal sync binlog err: %v", err)
		}
		return errors.Trace(err)
	}

	return nil
}

func (c *Canal) isClosed() bool {
	return c.closed.Get()
}

func (c *Canal) Close() {
	log.Infof("close canal")

	c.m.Lock()
	defer c.m.Unlock()

	if c.isClosed() {
		return
	}

	c.closed.Set(true)

	close(c.quit)

	c.connLock.Lock()
	c.conn.Close()
	c.conn = nil
	c.connLock.Unlock()

	if c.syncer != nil {
		c.syncer.Close()
		c.syncer = nil
	}

	c.master.Close()

	c.wg.Wait()
}

func (c *Canal) WaitDumpDone() <-chan struct{} {
	return c.dumpDoneCh
}

func (c *Canal) GetTable(db string, table string) (*schema.Table, error) {
	key := fmt.Sprintf("%s.%s", db, table)
	c.tableLock.Lock()
	t, ok := c.tables[key]
	c.tableLock.Unlock()

	if ok {
		log.Debug("GetTable exist the Talbe !! \n ")
		return t, nil
	}

	log.Info("GetTable do not exist the Table !! \n")
	t, err := schema.NewTable(c, db, table)
	if err != nil {
		log.Info("schema.NewTalbe Worng !\n")
		return nil, errors.Trace(err)
	}

	c.tableLock.Lock()
	c.tables[key] = t
	c.SaveTableSchema()
	c.tableLock.Unlock()

	return t, nil
}

// ClearTableCache clear table cache
func (c *Canal) ClearTableCache(db []byte, table []byte) {
	key := fmt.Sprintf("%s.%s", db, table)
	c.tableLock.Lock()
	delete(c.tables, key)
	c.SaveTableSchema()
	c.tableLock.Unlock()
}

// 保存表的结构体
func (c *Canal) SaveTableSchema() error {
	//打开或新建文件
	file, err := os.Create(path.Join(c.cfg.DataDir, "TableSchema.frm.tmp"))
	if err != nil {
		return err
	}
	defer file.Close()

	lf := json.NewEncoder(file)

	//将内容逐一写入文件

	for _, t := range c.tables {
		if err := lf.Encode(t); err != nil {
			log.Error(err.Error())
			return err
		}
	}
	os.Rename(path.Join(c.cfg.DataDir, "TableSchema.frm.tmp"), path.Join(c.cfg.DataDir, "TableSchema.frm"))

	return nil
}

func (c *Canal) ParseTableSchema() error {

	// 查看该结构文件是否存在，若不存在则返回
	exists := IsFileExists(path.Join(c.cfg.DataDir, "TableSchema.frm"))
	if !exists {
		log.Warning("TableSchema.frm do not exists,If you are FirsetDump ,it maybe right. \n If not,Please check why it was delete. \n Maybe it will make mistake while parse binlog for Talbe haved Alter or Rename !!!")
		return nil
	}

	log.Info("Start ParseTableSchema !!! \n")
	// 打开文件
	file, err := os.OpenFile(path.Join(c.cfg.DataDir, "TableSchema.frm"), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Error(err)
		return err
	}
	defer file.Close()

	filerReader := bufio.NewReader(file)

	for {
		line, err := filerReader.ReadBytes('\n')
		if err == io.EOF {
			log.Infof("Tableschema length is %d", len(c.tables))
			log.Info("Read TalbeSchema.frm Over! \n")
			return nil
		} else if err != nil {
			log.Error("Read TableSchema.frm Error! \n")
			return err
		}

		tablebyte := line[0 : len(line)-1]

		table, err := parseJsonToTableSchema(tablebyte)
		if err != nil {
			log.Error("Parse TableSchema.frm Error! \n")
			return err
		}

		key := fmt.Sprintf("%s.%s", table.Schema, table.Name)

		c.tableLock.Lock()
		c.tables[key] = table
		c.tableLock.Unlock()

		log.Debugf("c.tables length is %d", len(c.tables))
	}
}

func parseJsonToTableSchema(body []byte) (*schema.Table, error) {
	var table schema.Table
	err := json.Unmarshal(body, &table)
	if err != nil {
		return nil, err
	}
	return &table, nil
}

// Check MySQL binlog row image, must be in FULL, MINIMAL, NOBLOB
func (c *Canal) CheckBinlogRowImage(image string) error {
	// need to check MySQL binlog row image? full, minimal or noblob?
	// now only log
	if c.cfg.Flavor == mysql.MySQLFlavor {
		if res, err := c.Execute(`SHOW GLOBAL VARIABLES LIKE "binlog_row_image"`); err != nil {
			return errors.Trace(err)
		} else {
			// MySQL has binlog row image from 5.6, so older will return empty
			rowImage, _ := res.GetString(0, 1)
			if rowImage != "" && !strings.EqualFold(rowImage, image) {
				return errors.Errorf("MySQL uses %s binlog row image, but we want %s", rowImage, image)
			}
		}
	}

	return nil
}

func (c *Canal) checkBinlogRowFormat() error {
	res, err := c.Execute(`SHOW GLOBAL VARIABLES LIKE "binlog_format";`)
	if err != nil {
		return errors.Trace(err)
	} else if f, _ := res.GetString(0, 1); f != "ROW" {
		return errors.Errorf("binlog must ROW format, but %s now", f)
	}

	return nil
}

func (c *Canal) prepareSyncer() error {
	seps := strings.Split(c.cfg.Addr, ":")
	if len(seps) != 2 {
		return errors.Errorf("invalid mysql addr format %s, must host:port", c.cfg.Addr)
	}

	port, err := strconv.ParseUint(seps[1], 10, 16)
	if err != nil {
		return errors.Trace(err)
	}

	cfg := replication.BinlogSyncerConfig{
		ServerID: c.cfg.ServerID,
		Flavor:   c.cfg.Flavor,
		Host:     seps[0],
		Port:     uint16(port),
		User:     c.cfg.User,
		Password: c.cfg.Password,
	}

	c.syncer = replication.NewBinlogSyncer(&cfg)

	return nil
}

func (c *Canal) masterInfoPath() string {
	return path.Join(c.cfg.DataDir, "master.info")
}

// Execute a SQL
func (c *Canal) Execute(cmd string, args ...interface{}) (rr *mysql.Result, err error) {
	c.connLock.Lock()
	defer c.connLock.Unlock()

	retryNum := 3
	for i := 0; i < retryNum; i++ {
		if c.conn == nil {
			c.conn, err = client.Connect(c.cfg.Addr, c.cfg.User, c.cfg.Password, "")
			if err != nil {
				return nil, errors.Trace(err)
			}
		}

		rr, err = c.conn.Execute(cmd, args...)
		if err != nil && !mysql.ErrorEqual(err, mysql.ErrBadConn) {
			return
		} else if mysql.ErrorEqual(err, mysql.ErrBadConn) {
			c.conn.Close()
			c.conn = nil
			continue
		} else {
			return
		}
	}
	return
}

func (c *Canal) SyncedPosition() mysql.Position {
	return c.master.Pos()
}

func IsFileExists(name string) bool {
	f, err := os.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}

	if f.IsDir() {
		return false
	}

	return true
}
