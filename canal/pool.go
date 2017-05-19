package canal

import (
	"sync"
)

type Pool struct {
	work chan dumpParseHandlerGo
	wg   sync.WaitGroup
}

func NewPool(maxGoroutines int) *Pool {
	p := Pool{
		work: make(chan dumpParseHandlerGo, 1),
	}

	p.wg.Add(maxGoroutines)
	for i := 0; i < maxGoroutines; i++ {
		go func() {
			defer p.wg.Done()
			for w := range p.work {
				w.DataGo()
			}
		}()
	}
	return &p
}

func (p *Pool) Run(w dumpParseHandlerGo) {
	p.work <- w
}

func (p *Pool) Shutdown() {
	close(p.work)
	p.wg.Wait()
}
