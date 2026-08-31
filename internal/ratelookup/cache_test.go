// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ratelookup

import (
	"testing"
	"time"
)

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache(time.Second)
	c.Set("k", []byte("v"))
	got, ok := c.Get("k")
	if !ok || string(got) != "v" {
		t.Errorf("got %q ok=%v", got, ok)
	}
}

func TestCache_ExpiresAfterTTL(t *testing.T) {
	c := NewCache(10 * time.Millisecond)
	c.Set("k", []byte("v"))
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Error("expected expired entry")
	}
}

func TestCache_Delete(t *testing.T) {
	c := NewCache(time.Second)
	c.Set("k", []byte("v"))
	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Error("expected miss after delete")
	}
}

func TestCache_Purge(t *testing.T) {
	c := NewCache(time.Second)
	c.Set("a", []byte("1"))
	c.Set("b", []byte("2"))
	c.Purge()
	if c.Len() != 0 {
		t.Errorf("len after purge: %d", c.Len())
	}
}

func TestCache_Overwrite(t *testing.T) {
	c := NewCache(time.Second)
	c.Set("k", []byte("v1"))
	c.Set("k", []byte("v2"))
	got, _ := c.Get("k")
	if string(got) != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}
