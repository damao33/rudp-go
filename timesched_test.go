package rudp

import (
	"fmt"
	"testing"
	"time"
)

func TestPop(t *testing.T) {
	tf0 := timeFunc{
		execute: func() {
			fmt.Println("tf0")
		},
		ts: time.Now(),
	}
	tf1 := timeFunc{
		execute: func() {
			fmt.Println("tf1")
		},
		ts: time.Now(),
	}
	tf2 := timeFunc{
		execute: func() {
			fmt.Println("tf2")
		},
		ts: time.Now(),
	}
	var heap timeFuncHeap
	heap = append(heap, tf0)
	heap = append(heap, tf1)
	heap = append(heap, tf2)
	fmt.Println("before pop:")
	for _, tf := range heap {
		tf.execute()
	}
	pop := heap.Pop().(timeFunc)
	fmt.Println("after pop:")
	for _, tf := range heap {
		tf.execute()
	}
	fmt.Println("pop:")
	pop.execute()
}

func TestSched1(t *testing.T) {
	f := func() {
		fmt.Println("f running:", time.Now())
	}
	f2 := func() {
		fmt.Println("f2 running:", time.Now())
	}
	timer := time.NewTimer(5 * time.Second)
	SysTimeSched.Put(f, time.Now().Add(time.Second*3))
	SysTimeSched.Put(f2, time.Now())
	for {
		select {
		case <-timer.C:
			fmt.Println("timeout:", time.Now())
			return
		default:
		}
	}
}
