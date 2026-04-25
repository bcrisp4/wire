package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew_JSON(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(&buf, "json", "info")
	assert.NoError(t, err)
	l.Info("hello", "key", "value")
	out := buf.String()
	assert.Contains(t, out, `"msg":"hello"`)
	assert.Contains(t, out, `"key":"value"`)
}

func TestNew_Text(t *testing.T) {
	var buf bytes.Buffer
	l, err := New(&buf, "text", "info")
	assert.NoError(t, err)
	l.Info("hello", "key", "value")
	out := buf.String()
	assert.True(t, strings.Contains(out, "hello"))
	assert.True(t, strings.Contains(out, "key=value"))
}

func TestNew_DebugLevelEmitsDebug(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", "debug")
	l.Debug("d")
	assert.Contains(t, buf.String(), `"msg":"d"`)
}

func TestNew_InfoLevelDropsDebug(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, "json", "info")
	l.Debug("d")
	assert.Empty(t, buf.String())
}

func TestNew_RejectsBadLevel(t *testing.T) {
	_, err := New(nil, "json", "loud")
	assert.Error(t, err)
}

func TestNew_RejectsBadFormat(t *testing.T) {
	var buf bytes.Buffer
	_, err := New(&buf, "yaml", "info")
	assert.Error(t, err)
}
