package canal

import (
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/juju/errors"
)

type DumpConfig struct {
	// mysqldump execution path, like mysqldump or /usr/bin/mysqldump, etc...
	// If not set, ignore using mysqldump.
	ExecutionPath string `toml:"mysqldump"`

	// fordbs
	Databases []string `toml:"dbs"`

	// Will override Databases, tables is in database table_db
	Tables  []string `toml:"tables"`
	TableDB string   `toml:"table_db"`

	// Ignore table format is db.table
	IgnoreTables []string `toml:"ignore_tables"`

	// If true, discard error msg, else, output to stderr
	DiscardErr bool `toml:"discard_err"`
}

type Config struct {
	Addr     string `toml:"addr"`
	User     string `toml:"user"`
	Password string `toml:"password"`

	ServerID          uint32     `toml:"server_id"`
	Flavor            string     `toml:"flavor"`
	DataDir           string     `toml:"data_dir"`
	Taddr             string     `toml:"taddr"`
	Tuser             string     `toml:"tuser"`
	Tpassword         string     `toml:"tpassword"`
	dumpThreads       uint32     `toml:"dumpthreads"`
	LogFile           string     `toml:"logfile"`
	LogLevel          string     `toml:"log_level"`
	LogDir            string     `toml:"log_dir"`
	Dump              DumpConfig `toml:"dump"`
	AUTH              string     `toml:"auth"`
	MonitorIP         string     `toml:"monitorip"`
	MnotiroClientPort string     `toml:"monitorclient"`
}

func NewConfigWithFile(name string) (*Config, error) {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return NewConfig(string(data))
}

func NewConfig(data string) (*Config, error) {
	var c Config

	_, err := toml.Decode(data, &c)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return &c, nil
}

func NewDefaultConfig() *Config {
	c := new(Config)

	c.Addr = "127.0.0.1:3306"
	c.User = "root"
	c.Password = ""

	rand.Seed(time.Now().Unix())
	c.ServerID = uint32(rand.Intn(1000)) + 1001

	c.Flavor = "mysql"
	c.DataDir = "./var"
	c.Dump.ExecutionPath = "mysqldump"
	c.Dump.DiscardErr = true

	return c
}

func PathExist(path string) bool {
	_, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}
