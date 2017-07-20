package plugins

import (
	"database/sql"
	"fmt"
	"sync"

	"canal"

	_ "github.com/go-sql-driver/mysql"
	"github.com/ngaut/log"
	"github.com/siddontang/go-mysql/schema"
)

type DbSyncHandler struct {
	lock  sync.Mutex
	dbase *sql.DB
}

const ThreadforConn = 20

func NewDbSyncHandler(cfg *canal.Config) *DbSyncHandler {
	var tranferUri string
	tranferUri = fmt.Sprintf("%s:%s@tcp(%s)/%s", cfg.Tuser, cfg.Tpassword, cfg.Taddr, cfg.Dump.TableDB)
	db, _ := sql.Open("mysql", tranferUri+canal.MysqlTimeout)
	err := db.Ping()
	if err != nil {
		panic(err.Error())
	}
	db.SetMaxOpenConns(ThreadforConn)
	db.SetMaxIdleConns(5)
	handler := &DbSyncHandler{
		dbase: db}

	return handler
}

func (h *DbSyncHandler) Close() {
	h.dbase.Close()
}

func (h *DbSyncHandler) String() string {
	return "DbSyncHandler"
}

func (h *DbSyncHandler) Do(e *canal.RowsEvent) error {
	for i := 0; i < len(e.Rows); i++ {
		var err error
		if e.Action == canal.InsertAction {
			err = h.insert(e.Table, e.Rows[i])
		} else if e.Action == canal.UpdateAction {
			err = h.update(e.Table, e.Rows[i], e.Rows[i+1])
			i += 1
		} else if e.Action == canal.DeleteAction {
			err = h.delete(e.Table, e.Rows[i])
		} else {
			return nil
		}

		if err != nil {
			log.Errorf("handle data err: %v", err)
			return canal.ErrHandleInterrupted
		}
	}

	return nil
}

func (h *DbSyncHandler) insert(table *schema.Table, row []interface{}) error {
	var columns, values string
	for k := 0; k < len(table.Columns); k++ {
		if canal.FilterTabs[table.Name] {
			if !canal.FilterCols[table.Name][table.Columns[k].Name] {
				continue
			}
		}
		columns += "`" + table.Columns[k].Name + "`,"
		if row[k] == nil {
			values += "NULL,"
		} else {
			values += "'" + canal.EscapeStringBackslash(canal.InterfaceToString(row[k])) + "',"
		}
	}
	if columns == "" || values == "" {
		log.Infof("insert is empty: %s %s", columns, values)
		return nil
	}
	columns = columns[0 : len(columns)-1]
	values = values[0 : len(values)-1]

	sqlcmd := "REPLACE INTO `" + table.Schema + "`.`" + h.tableName(table.Name) + "` (" + columns + ") VALUES (" + values + ")"

	_, err := h.dbase.Exec(sqlcmd)
	// 关闭打印
	// log.Infof("Exec sql: %s, err: %v", sqlcmd, err)
	if err != nil {
		return fmt.Errorf("Exec sql(%s) Failed, err: %v", sqlcmd, err)
	}

	return nil
}

func (h *DbSyncHandler) delete(table *schema.Table, row []interface{}) error {
	var condition string
	for _, k := range table.PKColumns {
		if row[k] == nil {
			condition += "`" + table.Columns[k].Name + "`=NULL AND "
		} else {
			condition += "`" + table.Columns[k].Name + "`='" + canal.EscapeStringBackslash(canal.InterfaceToString(row[k])) + "' AND "
		}
	}

	if condition == "" {
		log.Warnf("delete condition is empty ignore....")
		return nil
	}
	condition = condition[0 : len(condition)-len(" AND ")]

	sqlcmd := "DELETE FROM `" + table.Schema + "`.`" + h.tableName(table.Name) + "` WHERE " + condition
	_, err := h.dbase.Exec(sqlcmd)
	// 关闭打印
	// log.Infof("Exec sql: %s, err: %v", sqlcmd, err)
	if err != nil {
		return fmt.Errorf("Exec sql(%s) Failed, err: %v", sqlcmd, err)
	}

	return nil
}

func (h *DbSyncHandler) update(table *schema.Table, before, after []interface{}) error {
	var condition, setValues string
	for _, k := range table.PKColumns {
		if before[k] == nil {
			condition += "`" + table.Columns[k].Name + "`=NULL AND "
		} else {
			condition += "`" + table.Columns[k].Name + "`='" +
				canal.EscapeStringBackslash(canal.InterfaceToString(before[k])) + "' AND "
		}
	}

	if condition == "" {
		log.Warnf("update condition is empty ignore....")
		return nil
	}
	condition = condition[0 : len(condition)-len(" AND ")]

	for k := 0; k < len(table.Columns); k++ {

		if canal.FilterTabs[table.Name] {
			if !canal.FilterCols[table.Name][table.Columns[k].Name] {
				continue
			}
		}
		if after[k] == nil {
			setValues += "`" + table.Columns[k].Name + "`=NULL,"
		} else {
			setValues += "`" + table.Columns[k].Name + "`='" + canal.EscapeStringBackslash(canal.InterfaceToString(after[k])) + "',"
		}
	}
	setValues = setValues[0 : len(setValues)-1]

	sqlcmd := "UPDATE `" + table.Schema + "`.`" + h.tableName(table.Name) + "` SET" + setValues + " WHERE " + condition
	_, err := h.dbase.Exec(sqlcmd)
	// 关闭打印
	// log.Infof("Exec sql: %s, err: %v", sqlcmd, err)
	if err != nil {
		return fmt.Errorf("Exec sql(%s) Failed, err: %v", sqlcmd, err)
	}

	return nil
}

func (h *DbSyncHandler) tableName(srcName string) string {
	t, ok := canal.Cfg_Tc[srcName]
	if ok {
		return t
	} else {
		return srcName
	}
}
