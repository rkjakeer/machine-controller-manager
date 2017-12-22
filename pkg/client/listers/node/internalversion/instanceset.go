// This file was automatically generated by lister-gen

package internalversion

import (
	node "code.sapcloud.io/kubernetes/node-controller-manager/pkg/apis/node"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// InstanceSetLister helps list InstanceSets.
type InstanceSetLister interface {
	// List lists all InstanceSets in the indexer.
	List(selector labels.Selector) (ret []*node.InstanceSet, err error)
	// Get retrieves the InstanceSet from the index for a given name.
	Get(name string) (*node.InstanceSet, error)
	InstanceSetListerExpansion
}

// instanceSetLister implements the InstanceSetLister interface.
type instanceSetLister struct {
	indexer cache.Indexer
}

// NewInstanceSetLister returns a new InstanceSetLister.
func NewInstanceSetLister(indexer cache.Indexer) InstanceSetLister {
	return &instanceSetLister{indexer: indexer}
}

// List lists all InstanceSets in the indexer.
func (s *instanceSetLister) List(selector labels.Selector) (ret []*node.InstanceSet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*node.InstanceSet))
	})
	return ret, err
}

// Get retrieves the InstanceSet from the index for a given name.
func (s *instanceSetLister) Get(name string) (*node.InstanceSet, error) {
	key := &node.InstanceSet{ObjectMeta: v1.ObjectMeta{Name: name}}
	obj, exists, err := s.indexer.Get(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(node.Resource("instanceset"), name)
	}
	return obj.(*node.InstanceSet), nil
}
