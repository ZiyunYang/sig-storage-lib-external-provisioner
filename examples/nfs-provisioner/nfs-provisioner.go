package main

import (
	"errors"
	"flag"
	"fmt"
	linuxproc "github.com/c9s/goprocinfo/linux"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	glog "k8s.io/klog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/controller"
	"syscall"
)

const (
	provisionerNameKey = "PROVISIONER_NAME"
)

type nfsProvisioner struct {
	client kubernetes.Interface
}

const (
	mountPath = "/persistentvolumes"
)

var _ controller.Provisioner = &nfsProvisioner{}

func inMap(key string, m map[string]string) bool {
	_, ok := m[key]
	return ok
}

func pvName(tenant string, stack string, service string, name string) string {
	return fmt.Sprintf("%s-%s-%s-%s", tenant, stack, service, name)
}

func isMounted(mp string) bool {
	mps, err := linuxproc.ReadMounts("/proc/mounts")
	if err != nil {
		return false
	}
	for _, m := range mps.Mounts {
		if m.MountPoint == mp {
			return true
		}
	}
	return false
}

func mountPoint(server string, path string) string {
	return fmt.Sprintf("%s/%s/%s", mountPath, server, url.QueryEscape(path))
}

func ensureMount(server string, path string) (string, error) {
	mp := mountPoint(server, path)
	if isMounted(mp) {
		return mp, nil
	}
	if err := os.MkdirAll(mp, 0777); err != nil {
		return mp, err
	}
	// has to be deployed as priviliged container
	cmd := exec.Command("mount", fmt.Sprintf("%s:%s", server, path), mp)
	return mp, cmd.Run()
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *nfsProvisioner) Provision(options controller.ProvisionOptions) (*v1.PersistentVolume, error) {
	params := options.StorageClass.Parameters
	if !(inMap("nfsPath", params) && inMap("nfsServer", params)) {
		return nil, fmt.Errorf("nfsPath and nfsServer parameters required")
	}
	server := params["nfsServer"]
	path := params["nfsPath"]
	mp, err := ensureMount(server, path)
	if err != nil {
		return nil, fmt.Errorf("unable to mount NFS volume: " + err.Error())
	}
	tenant := options.PVC.Labels["io.wise2c.tenant"]
	stack := options.PVC.Labels["io.wise2c.stack"]
	service := options.PVC.Labels["io.wise2c.service"]
	pvName := pvName(tenant, stack, service, options.PVName)
	if err := os.MkdirAll(filepath.Join(mp, options.PVName), 0777); err != nil {
		return nil, errors.New("unable to create directory to provision new pv: " + err.Error())
	}

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				NFS: &v1.NFSVolumeSource{
					Server:   server,
					Path:     filepath.Join(path, pvName),
					ReadOnly: false,
				},
			},
		},
	}

	return pv, nil
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *nfsProvisioner) Delete(volume *v1.PersistentVolume) error {
	//ann, ok := volume.Annotations["hostPathProvisionerIdentity"]
	//if !ok {
	//	return errors.New("identity annotation not found on PV")
	//}
	//if ann != p.identity {
	//	return &controller.IgnoredError{Reason: "identity annotation on PV does not match ours"}
	//}

	server := volume.Spec.PersistentVolumeSource.NFS.Server
	// Path include the dynamic volume name
	path := path.Dir(volume.Spec.PersistentVolumeSource.NFS.Path)
	mp, err := ensureMount(server, path)
	if err != nil {
		glog.Errorf("Failed to mount %s:%s %s", server, path, mp)
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	return nil
}

func main() {
	syscall.Umask(0)

	flag.Parse()
	glog.InitFlags(flag.CommandLine)

	// Create an InClusterConfig and use it to create a client for the controller
	// to use to communicate with Kubernetes
	provisionerName := os.Getenv(provisionerNameKey)
	if provisionerName == "" {
		glog.Fatalf("environment variable %s is not set! Please set it.", provisionerNameKey)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	clientNFSProvisioner := &nfsProvisioner{}

	// Start the provision controller which will dynamically provision hostPath
	// PVs
	pc := controller.NewProvisionController(clientset, provisionerName, clientNFSProvisioner, serverVersion.GitVersion)
	pc.Run(wait.NeverStop)
}
