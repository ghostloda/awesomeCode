package k8s

import (
	"bytes"
	"context"
	"embed"
	"github.com/ghodss/yaml"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/klog/v2"
	ocmapiv1 "open-cluster-management.io/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"text/template"
)

//go:embed resources
var f embed.FS
var configFile = "../config"
var (
	Scheme = k8sruntime.NewScheme()
)

//更多细节可以参考源码地址 github.com\oam-dev\cluster-register@v1.0.4-0.20220325092210-cee4a3d3fb7d\pkg\spoke\register.go

func newConfigGetter(configV1 *clientcmdapiv1.Config) clientcmd.KubeconfigGetter {
	return func() (*clientcmdapi.Config, error) {
		newData, err := yaml.Marshal(configV1)
		if err != nil {
			return nil, err
		}

		// convert *clientcmdapiv1.config to *clientcmdapi.config
		config, err := clientcmd.Load(newData)
		if err != nil {
			return nil, err
		}
		return config, nil
	}
}

// Client create and return kube runtime client
func Client() client.Client{
	configData := ""
	kubeConfig := new(clientcmdapiv1.Config)
	if err := yaml.Unmarshal([]byte(configData), kubeConfig); err != nil {
		klog.Error(err)
	}

	spokeConfig, err := clientcmd.BuildConfigFromKubeconfigGetter("", newConfigGetter(kubeConfig))
	if err != nil {
		klog.Error(err)
	}

	newClient, err := client.New(spokeConfig, client.Options{})
	if err != nil {
		klog.Error(err)
	}

	return newClient

}

func Apply() {
	newClient := Client()
	files := []string{
		"resource/namespace_agent.yaml",
		"resource/namespace.yaml",
		"resource/cluster_role.yaml",
		"resource/cluster_role_binding.yaml",
		"resource/klusterlets.crd.yaml",
		"resource/service_account.yaml",
	}
	err := ApplyK8sResource(context.Background(), f, newClient, files)
	if err != nil {
		klog.Error(err)
	}
}



func ApplyK8sResource(ctx context.Context, f embed.FS, k8sClient client.Client, files []string) error {
	for _, file := range files {
		data, err := f.ReadFile(file)
		if err != nil {
			klog.Error(err, "Fail to read embed file ", "name:", file)
			return err
		}
		k8sObject := new(unstructured.Unstructured)
		err = yaml.Unmarshal(data, k8sObject)
		if err != nil {
			klog.Error(err, "Fail to unmarshal file", "name", file)
			return err
		}
		err = CreateOrUpdateResource(ctx, k8sClient, k8sObject)
		if err != nil {
			klog.InfoS("Fail to create resource", "object", klog.KObj(k8sObject), "apiVersion", k8sObject.GetAPIVersion(), "kind", k8sObject.GetKind())
			return err
		}
	}
	return nil
}

func CreateOrUpdateResource(ctx context.Context, k8sClient client.Client, resource *unstructured.Unstructured) error {
	objKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}
	if err := k8sClient.Get(ctx, objKey, resource); err != nil {
		if kerrors.IsNotFound(err) {
			return k8sClient.Create(ctx, resource)
		}
		return err
	}
	return k8sClient.Update(ctx, resource)
}
//crd资源
func ApplyKlusterlet(ctx context.Context, k8sClient client.Client, file string, cluster *Cluster) error {
	t, err := template.ParseFS(f, file)
	if err != nil {
		klog.Error(err, "Fail to get Template from file", "name", file)
		return err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, cluster)
	if err != nil {
		klog.Error(err, "Fail to render klusterlet")
		return err
	}

	klusterlet := new(ocmapiv1.Klusterlet)
	err = yaml.Unmarshal(buf.Bytes(), klusterlet)
	if err != nil {
		klog.Error(err, "Fail to Unmarshal klusterlet")
		return err
	}

	latestKlusterlet := &ocmapiv1.Klusterlet{}
	err = k8sClient.Get(ctx, client.ObjectKey{Name: klusterlet.Name, Namespace: klusterlet.Namespace}, latestKlusterlet)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.InfoS("create klusterlet", "object", klog.KObj(klusterlet))
			return k8sClient.Create(ctx, klusterlet)
		}
		return err
	}
	klusterlet.ResourceVersion = latestKlusterlet.ResourceVersion
	klog.InfoS("update klusterlet", "object", klog.KObj(klusterlet))
	return k8sClient.Update(ctx, klusterlet)
}

// Cluster 这里代码可以忽视 ,主要看上面对yaml文件的读写转换
type Cluster struct {
	Name string
	Args Args
	HubInfo
}

type Args struct {
	KubeConfig *rest.Config
	Schema     *runtime.Scheme
	Client     client.Client
}

type HubInfo struct {
	KubeConfig *clientcmdapiv1.Config
	APIServer  string
}
