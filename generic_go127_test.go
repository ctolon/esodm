//go:build go1.27

package esodm

import (
	"context"
	"testing"
)

func TestGenericMethods(t *testing.T) {
	c := testClient(nil)
	s, err := NewSchema[testDoc]("test")
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Repository(s)
	if err != nil || r.Schema() != s {
		t.Fatal(r, err)
	}
	out, err := c.Load[testDoc](context.Background(), nil)
	if err != nil || len(out) != 0 {
		t.Fatal(out, err)
	}
}
