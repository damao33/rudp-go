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
	s2 := s1[:0]
	s3 := s1[0:0]
	fmt.Println(len(s2), cap(s2), len(s3), cap(s3))
	fmt.Println(s1)
	fmt.Println(s2)
	fmt.Println(s3)
}
