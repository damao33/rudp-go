package main

import (
	"fmt"
	"testing"
)

type lis struct {
	name string
}

func TestMain1(t *testing.T) {
	l := &lis{"abc"}
	fmt.Println(l)
	l = transLis(l)
	fmt.Println(l)
}

func transLis(l *lis) *lis {
	l.name = "zxc"
	return l
}

type segment struct {
	data []byte
}

func TestSlice(t *testing.T) {
	s1 := []byte{1, 2, 3}
	s2 := []byte{4, 5, 6, 7}
	s1 = append(s1, s2[:2]...)
	fmt.Println(s1)
}
