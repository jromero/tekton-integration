package main_test

import (
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"text/template"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
	"github.com/sirupsen/logrus"
	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/create"
)

func TestIntegration(t *testing.T) {
	gomega.RegisterTestingT(t)
	spec.Run(t, "integration", testIntegration, spec.Report(report.Terminal{}))
}

const (
	clusterName  = "integration-test-cluster"
	registryName = "integration-test-registry"
)

func testIntegration(t *testing.T, when spec.G, it spec.S) {
	when("tekton is installed", func() {
		var (
			kindCtx       *cluster.Context
			registryPort  int
			tmpDir        string
			err           error
			cleanUpDocker = func() {
				_ = exec.Command("docker", "stop", registryName).Run()
				_ = exec.Command("docker", "rm", registryName).Run()
				_ = kindCtx.Delete()
			}
		)

		it.Before(func() {
			t.Log("===> BEFORE")
			tmpDir, err = ioutil.TempDir("", "integration-test")
			assertNil(t, "creating temp dir", err)

			kindCtx = cluster.NewContext(clusterName)
			cleanUpDocker()

			t.Log("Starting registry...")
			registryPort, err = startRegistry(registryName)
			assertNil(t, "starting registry", err)

			t.Log("Creating k8s cluster...")
			logrus.SetOutput(ioutil.Discard)
			err = kindCtx.Create(
				create.WaitForReady(time.Minute * 1),
			)
			assertNil(t, "creating kind context", err)

			t.Log("Configuring kubectl...")
			kubeConfigPath := kindCtx.KubeConfigPath()
			if kubeConfigPath == "" {
				t.Fatal("Kube Config path from kind is empty")
			}
			err = os.Setenv("KUBECONFIG", kubeConfigPath)
			assertNil(t, "setting KUBECONFIG", err)

			t.Log("Installing Tekton...")
			_, err = exec.Command("kubectl",
				"apply", "-f", "https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml",
			).CombinedOutput()
			assertNil(t, "installing tekton", err)

			t.Log("Waiting for Tekton pods to be READY...")
			waitForTekton(t, kubeConfigPath)
		})

		it.After(func() {
			t.Log("===> AFTER")
			if os.Getenv("SKIP_CLEANUP") == "true" {
				t.Logf(`==============
SKIPPING CLEANUP:
To manually clean up run 'kind delete cluster --name="%s"' 
or rerun tests without 'SKIP_CLEANUP=true' 

The temp dir is: %s
Registry URL is: localhost:%d
To use kubectl run: export KUBECONFIG="%s"
To list TaskRuns run: kubectl get taskruns
==============`,
					kindCtx.Name(),
					tmpDir,
					registryPort,
					kindCtx.KubeConfigPath(),
				)
				return
			}

			t.Log("Deleting temp dir...")
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("Deleting temp dir %s", tmpDir)
			}

			t.Log("Cleaning up docker...")
			cleanUpDocker()
		})

		it("should install and build app", func() {
			t.Log("===> INSTALL")
			t.Log("Installing 'buildpacks' TaskRun...")
			output, err := exec.Command("kubectl",
				"create", "-f", "https://raw.githubusercontent.com/tektoncd/catalog/master/buildpacks/buildpacks-v3.yaml",
			).CombinedOutput()
			assertNil(t, "installing buildpacks task", err)
			t.Log(string(bytes.TrimSpace(output)))

			t.Log("===> BUILD APP")
			
			t.Log("Finalizing build.yml...")
			ipAddress, err := resolveIPAddress()
			assertNil(t, "resolving IP address", err)
			templateContents, err := ioutil.ReadFile(filepath.Join("testdata", "build.tmpl.yml"))
			assertNil(t, "reading build template file", err)
			buildYAMLFile, err := ioutil.TempFile(tmpDir, "build.*.yml")
			assertNil(t, "creating build config", err)
			err = template.Must(template.New("").Parse(string(templateContents))).Execute(buildYAMLFile,
				map[string]string{
					"IPAddress":    ipAddress,
					"RegistryPort": strconv.Itoa(registryPort),
				})
			assertNil(t, "writing build config", err)

			t.Log("Creating build...")
			output, err = exec.Command("kubectl",
				"create", "-f", buildYAMLFile.Name(),
			).CombinedOutput()
			assertNil(t, "creating build on k8s", err)
			t.Log(string(bytes.TrimSpace(output)))

			t.Log("===> RUNNING APP")
			t.Log("TODO!!!")
		})
	})
}

func assertNil(t *testing.T, msg string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %s", msg, err)
	}
}

func waitForTekton(t *testing.T, kubeConfigPath string) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	assertNil(t, "creating k8s client-go config", err)

	clientset, err := kubernetes.NewForConfig(config)
	assertNil(t, "creating k8s client-go clientset", err)

	podsClient := clientset.CoreV1().Pods("tekton-pipelines")
	gomega.Eventually(func() bool {
		podsList, err := podsClient.List(v1.ListOptions{})
		assertNil(t, "listing pods", err)

		pods := podsList.Items

		if len(pods) < 1 {
			return false
		}

		for _, pod := range pods {
			if pod.Status.Phase != v12.PodRunning {
				return false
			}
		}

		return true
	}, 40*time.Second, 2*time.Second).Should(gomega.BeTrue())
}

func resolveIPAddress() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", errors.New("unable to resolve IP address")
}

func startRegistry(containerName string) (int, error) {
	port, err := freePort()
	if err != nil {
		return 0, errors.Wrap(err, "getting free port")
	}

	err = exec.Command("docker", "run", "-d", "--rm", "-p", strconv.Itoa(port)+":5000", "--name", containerName, "registry:2").Run()
	if err != nil {
		return 0, errors.Wrap(err, "starting registry")
	}

	return port, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}

	address, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.Errorf("unknown address type: %+v", address)
	}

	if err := l.Close(); err != nil {
		return 0, err
	}

	return address.Port, nil
}
