package common

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTagData(t *testing.T) {
	// Create a new TagData
	td := NewTagData()

	// Add some tags
	td.AddTag("key1", "value1")
	td.AddTag("key2", "value2")

	// Check that the tags were added
	assert.Equal(t, "value1", td.GetTag("key1"))
	assert.Equal(t, "value2", td.GetTag("key2"))

	// Check that the pack type is correct
	assert.Equal(t, int16(PACK_TAG_DATA), td.GetPackType())
}
