package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"canal"
	"plugins"
)

var (
	configFile = flag.String("c", "./datapipe.toml", "config file, Usage<-c file>")
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	flag.Parse()
	var cfg *canal.Config
	var err error
	if cfg, err = canal.NewConfigWithFile(*configFile); err != nil {
		log.Panicf("parse config file failed(%s): %s", *configFile, err)
	}

	if err := canal.ParseFilter(*configFile); err != nil {
		log.Panicf("parse filterclos file failed(%s): %s", *configFile, err)
	}

	if err := canal.ParseOptimus(*configFile); err != nil {
		log.Panicf("parse optimus file failed(%s): %s", *configFile, err)
	}

	go tranferData(cfg)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		os.Kill,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	<-sc
	canal.MasterSave.Save(true)
}

func tranferData(cfg *canal.Config) {
	for i := 0; ; i++ {
		time.Sleep(time.Second * 1)
		startCanal(cfg)
	}
}

func startCanal(cfg *canal.Config) bool {

	var c *canal.Canal
	c, err := canal.NewCanal(cfg)
	if err != nil {
		fmt.Printf("create canal err %v", err)
		os.Exit(1)
	}

	// fmt.Printf("after NewCanal create") //debug

	handler := getRowsEventHandler(cfg)
	ddlhandler := getQueryEventHandler(cfg)

	if handler == nil || ddlhandler == nil || c == nil {
		if handler != nil {
			handler.Close()
		}
		if ddlhandler != nil {
			ddlhandler.Close()
		}
		if c != nil {
			c.Close()
		}
		return false
	}

	c.RegRowsEventHandler(handler)
	c.RegQueryEventHandler(ddlhandler)

	canalquit := c.Start()
	defer ddlhandler.Close()
	defer handler.Close()
	defer c.Close()

	SaveTimer := time.NewTicker(2 * time.Second)
	defer SaveTimer.Stop()

	for {
		select {
		case <-SaveTimer.C:
			c.SaveConfig()

		case syncErr := <-canalquit:
			log.Printf("canal quit: %v", syncErr)
			return false
		}
	}

	return true
}

func getRowsEventHandler(cfg *canal.Config) canal.RowsEventHandler {
	//var handler canal.RowsEventHandler
	handler := plugins.NewDbSyncHandler(cfg)
	if handler != nil {
		return handler
	} else {
		return nil
	}
}

func getQueryEventHandler(cfg *canal.Config) canal.QueryEventHandler {
	var handler canal.QueryEventHandler
	handler = plugins.NewDbSyncQueryHandler(cfg)
	if handler != nil {
		return handler
	} else {
		return nil
	}
}
