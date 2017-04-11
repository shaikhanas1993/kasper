package kasper

import (
	"fmt"
	"strings"

	"golang.org/x/net/context"
	elastic "gopkg.in/olivere/elastic.v5"
)

const maxBulkErrorReasons = 5

const defaultIndexSettings = `{
	"index.translog.durability": "request"
}`

const defaultTypeMapping = `{
	"dynamic_templates": [{
		"no_index": {
			"mapping": {
				"index": "no"
			},
			"match": "*"
		}
	}]
}`

// ElasticsearchKeyValueStore is a key-value storage that uses ElasticSearch.
type ElasticsearchKeyValueStore struct {
	IndexSettings string
	TypeMapping   string

	client          *elastic.Client
	context         context.Context
	indexName       string
	typeName        string
}

// NewElasticsearchKeyValueStoreWithMetrics creates new ElasticsearchKeyValueStore instance.
// Host must of the format hostname:port.
// StructPtr should be a pointer to struct type that is used.
// for serialization and deserialization of store values.
func NewElasticsearchKeyValueStore(url, indexName, typeName string) *ElasticsearchKeyValueStore {
	client, err := elastic.NewClient(
		elastic.SetURL(url),
		elastic.SetSniff(false), // FIXME: workaround for issues with ES in docker
	)
	if err != nil {
		logger.Panicf("Cannot create ElasticSearch Client to '%s': %s", url, err)
	}
	logger.Info("Connected to Elasticsearch at ", url)
	s := &ElasticsearchKeyValueStore{
		client:          client,
		context:         context.Background(),
		indexName:       indexName,
		typeName:        typeName,
	}
	s.IndexSettings = defaultIndexSettings
	s.TypeMapping = defaultTypeMapping
	s.checkOrCreateIndex()
	s.checkOrPutMapping()
	return s
}

func (s *ElasticsearchKeyValueStore) checkOrCreateIndex() {
	exists, err := s.client.IndexExists(s.indexName).Do(s.context)
	if err != nil {
		logger.Panic(fmt.Sprintf("Failed to check if index exists: %s", err))
	}
	if !exists {
		logger.Infof("Creating index %s", s.indexName)
		_, err = s.client.
			CreateIndex(s.indexName).
			BodyString(s.IndexSettings).
			Do(s.context)
		if err != nil {
			logger.Panic(fmt.Sprintf("Failed to create index: %s", err))
		}
	}
}

func (s *ElasticsearchKeyValueStore) checkOrPutMapping() {
	getResp, err := s.client.GetMapping().
		Index(s.indexName).
		Type(s.typeName).
		Do(s.context)
	if err != nil {
		logger.Panicf("Failed to get mapping for %s/%s: %s", s.indexName, s.typeName, err)
	}

	_, found := getResp[s.typeName]
	if found {
		return
	}

	putResp, err := s.client.
		PutMapping().
		Index(s.indexName).
		Type(s.typeName).
		BodyString(s.TypeMapping).
		Do(s.context)
	if err != nil {
		logger.Panic(fmt.Sprintf("Failed to put mapping for %s/%s: %s", s.indexName, s.typeName, err))
	}
	if putResp == nil {
		logger.Panic(fmt.Sprintf("Expected put mapping response; got: %v", putResp))
	}
	if !putResp.Acknowledged {
		logger.Panic(fmt.Sprintf("Expected put mapping ack; got: %v", putResp.Acknowledged))
	}
}

// Get gets value by key from store
func (s *ElasticsearchKeyValueStore) Get(key string) ([]byte, error) {
	logger.Debug("Elasticsearch Get: ", key)
	rawValue, err := s.client.Get().
		Index(s.indexName).
		Type(s.typeName).
		Id(key).
		Do(s.context)

	if fmt.Sprintf("%s", err) == "elastic: Error 404 (Not Found)" {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if !rawValue.Found {
		return nil, nil
	}

	return *rawValue.Source, nil
}

// GetAll gets multiple keys from store using MultiGet.
func (s *ElasticsearchKeyValueStore) GetAll(keys []string) ([]KeyValue, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	logger.Debug("Elasticsearch GetAll: ", keys)
	multiGet := s.client.MultiGet()
	for _, key := range keys {

		item := elastic.NewMultiGetItem().
			Index(s.indexName).
			Type(s.typeName).
			Id(key)

		multiGet.Add(item)
	}
	response, err := multiGet.Do(s.context)
	if err != nil {
		return nil, err
	}
	kvs := make([]KeyValue, len(keys))
	for i, doc := range response.Docs {
		if doc.Found {
			kvs[i] = KeyValue{keys[i], *doc.Source}
		}
	}
	return kvs, nil
}

// Put updates key in store with serialized value
func (s *ElasticsearchKeyValueStore) Put(key string, value []byte) error {
	logger.Debug(fmt.Sprintf("Elasticsearch Put: %s/%s/%s %#v", s.indexName, s.typeName, key, value))

	_, err := s.client.Index().
		Index(s.indexName).
		Type(s.typeName).
		Id(key).
		BodyString(string(value)).
		Do(s.context)

	return err
}

// PutAll bulk executes Put operation for several kvs
func (s *ElasticsearchKeyValueStore) PutAll(kvs []KeyValue) error {
	logger.Debugf("Elasticsearch PutAll of %d keys", len(kvs))
	if len(kvs) == 0 {
		return nil
	}
	bulk := s.client.Bulk()
	for _, kv := range kvs {
		bulk.Add(elastic.NewBulkIndexRequest().
			Index(s.indexName).
			Type(s.typeName).
			Id(kv.Key).
			Doc(string(kv.Value)),
		)
	}
	response, err := bulk.Do(s.context)
	if err != nil {
		return err
	}
	if response.Errors {
		return createBulkError(response)
	}
	return nil
}

// Delete removes key from store
func (s *ElasticsearchKeyValueStore) Delete(key string) error {
	logger.Debug("Elasticsearch Delete: ", key)

	_, err := s.client.Delete().
		Index(s.indexName).
		Type(s.typeName).
		Id(key).
		Do(s.context)

	if err != nil && err.(*elastic.Error).Status == 404 {
		return nil
	}

	return err
}

// Flush the Elasticsearch translog to disk
func (s *ElasticsearchKeyValueStore) Flush() error {
	logger.Info("Elasticsearch Flush...")
	_, err := s.client.Flush("_all").
		WaitIfOngoing(true).
		Do(s.context)
	logger.Info("Elasticsearch Flush complete")
	return err
}

// GetClient return underlying elastic.Client
func (s *ElasticsearchKeyValueStore) GetClient() *elastic.Client {
	return s.client
}

func (s* ElasticsearchKeyValueStore) WithMetrics(provider MetricsProvider, label string) KeyValueStore {
	return NewStoreMetrics(s, provider, label)
}


func createBulkError(response *elastic.BulkResponse) error {
	reasons := []string{}
	failed := response.Failed()
	for i, item := range failed {
		if item.Error != nil {
			reason := fmt.Sprintf("id = %s, error = %s\n", item.Id, item.Error.Reason)
			reasons = append(reasons, reason)
		}
		if i == maxBulkErrorReasons-1 {
			reason := fmt.Sprintf("(omitted %d more errors)", len(failed)-maxBulkErrorReasons)
			reasons = append(reasons, reason)
			break
		}
	}
	err := fmt.Errorf("PutAll failed for some requests:\n%s", strings.Join(reasons, ""))
	return err
}
