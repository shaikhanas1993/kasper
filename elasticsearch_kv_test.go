package kasper

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

type Dragon struct {
	Color string `json:"color"`
	Name  string `json:"name"`
}

var store *ElasticsearchKeyValueStore

func TestElasticsearchKeyValue_Get_Put(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	// Get non-existing key
	item, err := store.Get("kasper/dragon/vorgansharax")
	assert.Nil(t, item)
	assert.Nil(t, err)

	// Put key
	err = store.Put("kasper/dragon/vorgansharax", &Dragon{"green", "Vorgansharax"})
	assert.Nil(t, err)

	// Get key again, should find it this time
	item, err = store.Get("kasper/dragon/vorgansharax")
	assert.NotNil(t, item)
	assert.Nil(t, err)
	dragon := item.(*Dragon)
	assert.Equal(t, &Dragon{"green", "Vorgansharax"}, dragon)
}

func TestElasticsearchKeyValueStore_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	// Put key
	err := store.Put("kasper/dragon/falkor", &Dragon{"white", "Falkor"})
	assert.Nil(t, err)

	// Delete key
	err = store.Delete("kasper/dragon/falkor")
	assert.Nil(t, err)

	// Get key again, should not find it this time
	item, err := store.Get("kasper/dragon/falkor")
	assert.Nil(t, err)
	assert.Nil(t, item)

	// Delete key again does nothing
	err = store.Delete("kasper/dragon/falkor")
	assert.Nil(t, err)
}

func TestElasticsearchKeyValueStore_GetAll_PutAll(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	// Put 3 keys
	err := store.Put("kasper/dragon/saphira", &Dragon{"blue", "Saphira"})
	assert.Nil(t, err)
	err = store.Put("kasper/dragon/mushu", &Dragon{"red", "Mushu"})
	assert.Nil(t, err)
	err = store.Put("kasper/dragon/fin-fang-foom", &Dragon{"green", "Fin Fang Foom"})
	assert.Nil(t, err)

	// GetAll on 4 keys, one non existing
	kvs, err := ToMap(store.GetAll([]string{
		"kasper/dragon/saphira",
		"kasper/dragon/draco",
		"kasper/dragon/mushu",
		"kasper/dragon/fin-fang-foom",
	}))
	assert.Nil(t, err)

	// Check the 3 keys
	assert.Equal(t, 3, len(kvs))
	assert.Equal(t, &Dragon{"blue", "Saphira"}, kvs["kasper/dragon/saphira"])
	assert.Equal(t, &Dragon{"red", "Mushu"}, kvs["kasper/dragon/mushu"])
	assert.Equal(t, &Dragon{"green", "Fin Fang Foom"}, kvs["kasper/dragon/fin-fang-foom"])

	// Delete everything
	_, err = store.client.DeleteIndex("kasper").Do(store.context)
	assert.Nil(t, err)

	// PutAll all 3 dragons again
	err = store.PutAll(FromMap(kvs))
	assert.Nil(t, err)

	// Check the 3 keys once more
	kvs, err = ToMap(store.GetAll([]string{
		"kasper/dragon/saphira",
		"kasper/dragon/mushu",
		"kasper/dragon/fin-fang-foom",
	}))
	assert.Nil(t, err)
	assert.Equal(t, 3, len(kvs))
	assert.Equal(t, &Dragon{"blue", "Saphira"}, kvs["kasper/dragon/saphira"])
	assert.Equal(t, &Dragon{"red", "Mushu"}, kvs["kasper/dragon/mushu"])
	assert.Equal(t, &Dragon{"green", "Fin Fang Foom"}, kvs["kasper/dragon/fin-fang-foom"])
}

func TestElasticsearchKeyValueStore_Flush(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	err := store.Flush()
	assert.Nil(t, err)
}

func TestElasticsearchKeyValueStore_InvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	assert.Panics(t, func() {
		store.Get("foo")
	})
	assert.Panics(t, func() {
		store.Put("foo", &Dragon{})
	})
	assert.Panics(t, func() {
		store.GetAll([]string{"foo"})
	})
	assert.Panics(t, func() {
		store.PutAll([]*KeyValue{{"foo", &Dragon{}}})
	})
	assert.Panics(t, func() {
		store.Delete("foo")
	})
}

func init() {
	SetLogger(&noopLogger{})
	if ! testing.Short() {
		store = NewElasticsearchKeyValueStore("http://localhost:9200", &Dragon{})
		store.client.DeleteIndex("kasper").Do(store.context)
	}
}