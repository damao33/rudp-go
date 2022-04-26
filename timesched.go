package rudp

import (
	"container/heap"
	"runtime"
	"sync"
	"time"
)

var SysTimeSched = NewTimedSched(runtime.NumCPU())

type (
	timeFunc struct {
		execute func()
		ts      time.Time
	}
	// 存放定时函数的堆
	timeFuncHeap []timeFunc
)

func (h timeFuncHeap) Len() int {
	return len(h)
}

func (h timeFuncHeap) Less(i, j int) bool {
	return h[i].ts.Before(h[j].ts)
}

func (h timeFuncHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *timeFuncHeap) Push(x interface{}) {
	*h = append(*h, x.(timeFunc))
}

func (h *timeFuncHeap) Pop() (x interface{}) {
	n := len(*h)
	x = (*h)[n-1]
	// 避免内存泄漏
	(*h)[n-1].execute = nil
	*h = (*h)[:n-1]
	return
}

// TimedSched 控制并行任务的调度器
type TimedSched struct {
	prependTasks    []timeFunc
	prependLock     sync.Mutex
	chPrependNotify chan struct{}

	chTask chan timeFunc

	die     chan struct{}
	dieOnce sync.Once
}

//
// NewTimedSched
// @Description:
// @param parallel 并行度
// @return *TimedSched
//
func NewTimedSched(parallel int) *TimedSched {
	ts := &TimedSched{
		chPrependNotify: make(chan struct{}, 1),
		chTask:          make(chan timeFunc),
		die:             make(chan struct{}),
	}
	for i := 0; i < parallel; i++ {
		go ts.sched()
	}
	go ts.prepend()
	return ts
}

// sched 按时执行定时任务
func (ts *TimedSched) sched() {
	var tasks timeFuncHeap
	timer := time.NewTimer(0)
	allDone := false
	for {
		select {
		case task := <-ts.chTask: // 收到新任务
			now := time.Now()
			if now.After(task.ts) {
				// 已经超时，立刻执行任务
				task.execute()
			} else {
				heap.Push(&tasks, task)
				// 根据堆顶任务重置定时器
				stopped := timer.Stop()
				if !stopped && !allDone {
					<-timer.C
				}
				timer.Reset(tasks[0].ts.Sub(now))
				allDone = false
			}
		case now := <-timer.C: // 计时器超时
			allDone = true
			for len(tasks) > 0 {
				if now.After(tasks[0].ts) {
					heap.Pop(&tasks).(timeFunc).execute()
				} else {
					timer.Reset(tasks[0].ts.Sub(now))
					allDone = false
					break
				}
			}
		case <-ts.die:
			return
		}
	}
}

// prepend 准备任务
func (ts *TimedSched) prepend() {
	var tasks []timeFunc
	for {
		select {
		case <-ts.chPrependNotify:
			ts.prependLock.Lock()
			// 利用内存
			if cap(tasks) < cap(ts.prependTasks) {
				tasks = make([]timeFunc, 0, cap(ts.prependTasks))
			}
			tasks = tasks[:len(ts.prependTasks)]
			copy(tasks, ts.prependTasks)
			for i := range ts.prependTasks {
				// 避免内存泄露
				ts.prependTasks[i].execute = nil
			}
			ts.prependTasks = ts.prependTasks[:0]
			ts.prependLock.Unlock()

			for i := range tasks {
				select {
				case ts.chTask <- tasks[i]:
					tasks[i].execute = nil
				case <-ts.die:
					return
				}
			}
			tasks = tasks[:0]
		case <-ts.die:
			return
		}
	}
}

//
// Put
// @Description: f任务在ddl时被执行
// @receiver ts
// @param f	添加的任务
// @param ddl 任务的截至时间
//
func (ts *TimedSched) Put(f func(), ddl time.Time) {
	ts.prependLock.Lock()
	ts.prependTasks = append(ts.prependTasks, timeFunc{f, ddl})
	ts.prependLock.Unlock()

	select {
	case ts.chPrependNotify <- struct{}{}:
	default:
	}
}

func (ts *TimedSched) Close() {
	ts.dieOnce.Do(func() {
		close(ts.die)
	})
}
