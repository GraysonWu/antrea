// Copyright 2020 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	scheme "github.com/vmware-tanzu/antrea/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// AntreaClusterNetworkPolicyMetricsesGetter has a method to return a AntreaClusterNetworkPolicyMetricsInterface.
// A group's client should implement this interface.
type AntreaClusterNetworkPolicyMetricsesGetter interface {
	AntreaClusterNetworkPolicyMetricses() AntreaClusterNetworkPolicyMetricsInterface
}

// AntreaClusterNetworkPolicyMetricsInterface has methods to work with AntreaClusterNetworkPolicyMetrics resources.
type AntreaClusterNetworkPolicyMetricsInterface interface {
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.AntreaClusterNetworkPolicyMetrics, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.AntreaClusterNetworkPolicyMetricsList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	AntreaClusterNetworkPolicyMetricsExpansion
}

// antreaClusterNetworkPolicyMetricses implements AntreaClusterNetworkPolicyMetricsInterface
type antreaClusterNetworkPolicyMetricses struct {
	client rest.Interface
}

// newAntreaClusterNetworkPolicyMetricses returns a AntreaClusterNetworkPolicyMetricses
func newAntreaClusterNetworkPolicyMetricses(c *MetricsV1alpha1Client) *antreaClusterNetworkPolicyMetricses {
	return &antreaClusterNetworkPolicyMetricses{
		client: c.RESTClient(),
	}
}

// Get takes name of the antreaClusterNetworkPolicyMetrics, and returns the corresponding antreaClusterNetworkPolicyMetrics object, and an error if there is any.
func (c *antreaClusterNetworkPolicyMetricses) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.AntreaClusterNetworkPolicyMetrics, err error) {
	result = &v1alpha1.AntreaClusterNetworkPolicyMetrics{}
	err = c.client.Get().
		Resource("antreaclusternetworkpolicymetrics").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of AntreaClusterNetworkPolicyMetricses that match those selectors.
func (c *antreaClusterNetworkPolicyMetricses) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.AntreaClusterNetworkPolicyMetricsList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.AntreaClusterNetworkPolicyMetricsList{}
	err = c.client.Get().
		Resource("antreaclusternetworkpolicymetrics").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested antreaClusterNetworkPolicyMetricses.
func (c *antreaClusterNetworkPolicyMetricses) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("antreaclusternetworkpolicymetrics").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}
