package anim

// job is one packed frame handed from the render goroutine to the flush
// goroutine: the pooled RGB565 buffer plus the sub-rectangle within it to push
// to the panel.
type job struct {
	buf  []byte
	rect Rect
}

// runFlusher is the flush goroutine and the sole owner of the panel. It drains
// packed frames, streams each dirty rectangle out through the RectFlusher, and
// returns the buffer to the pool. It exits when ready is closed (after the
// render loop has stopped), signalling flushDone. Because the render side feeds
// it through a capacity-1 latest-wins channel, a slow flush drops stale frames
// rather than back-pressuring the render loop.
func (e *Engine) runFlusher() {
	defer close(e.flushDone)
	for j := range e.ready {
		if err := e.flush.FlushRect(j.buf, j.rect.X0, j.rect.Y0, j.rect.X1, j.rect.Y1); err != nil {
			e.log.Warn("anim: flush failed", "err", err, "rect", j.rect)
		}
		e.nFlushed.Add(1)
		e.free <- j.buf
	}
}
