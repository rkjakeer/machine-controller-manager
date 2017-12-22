// This file was automatically generated by informer-gen

package v1alpha1

import (
	node_v1alpha1 "code.sapcloud.io/kubernetes/node-controller-manager/pkg/apis/node/v1alpha1"
	clientset "code.sapcloud.io/kubernetes/node-controller-manager/pkg/client/clientset"
	internalinterfaces "code.sapcloud.io/kubernetes/node-controller-manager/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "code.sapcloud.io/kubernetes/node-controller-manager/pkg/client/listers/node/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
	time "time"
)

// AWSInstanceClassInformer provides access to a shared informer and lister for
// AWSInstanceClasses.
type AWSInstanceClassInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.AWSInstanceClassLister
}

type aWSInstanceClassInformer struct {
	factory internalinterfaces.SharedInformerFactory
}

// NewAWSInstanceClassInformer constructs a new informer for AWSInstanceClass type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewAWSInstanceClassInformer(client clientset.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				return client.NodeV1alpha1().AWSInstanceClasses().List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				return client.NodeV1alpha1().AWSInstanceClasses().Watch(options)
			},
		},
		&node_v1alpha1.AWSInstanceClass{},
		resyncPeriod,
		indexers,
	)
}

func defaultAWSInstanceClassInformer(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewAWSInstanceClassInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func (f *aWSInstanceClassInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&node_v1alpha1.AWSInstanceClass{}, defaultAWSInstanceClassInformer)
}

func (f *aWSInstanceClassInformer) Lister() v1alpha1.AWSInstanceClassLister {
	return v1alpha1.NewAWSInstanceClassLister(f.Informer().GetIndexer())
}
