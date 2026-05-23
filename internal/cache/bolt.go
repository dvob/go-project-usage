package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("projects")

type BoltCache struct {
	db *bolt.DB
}

func NewBoltCache(path string) (*BoltCache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &BoltCache{db: db}, nil
}

func (c *BoltCache) Get(repos []string, maxAge time.Duration) (map[string]RepoInfo, error) {
	result := make(map[string]RepoInfo)
	cutoff := time.Now().Add(-maxAge)

	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		for _, repo := range repos {
			v := b.Get([]byte(repo))
			if v == nil {
				continue
			}
			var p RepoInfo
			if err := json.Unmarshal(v, &p); err != nil {
				continue
			}
			if p.FetchedAt.Before(cutoff) {
				continue
			}
			result[repo] = p
		}
		return nil
	})
	return result, err
}

func (c *BoltCache) Put(projects []RepoInfo) error {
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		for _, p := range projects {
			data, err := json.Marshal(p)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(p.Name), data); err != nil {
				return err
			}
		}
		return nil
	})
}

func (c *BoltCache) List() ([]RepoInfo, error) {
	var projects []RepoInfo
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		return b.ForEach(func(k, v []byte) error {
			var p RepoInfo
			if err := json.Unmarshal(v, &p); err != nil {
				return nil
			}
			projects = append(projects, p)
			return nil
		})
	})
	return projects, err
}

func (c *BoltCache) Close() error {
	return c.db.Close()
}
