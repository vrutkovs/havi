package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	tkn "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	tknClient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1beta1"

	"github.com/coreos/go-semver/semver"
)

const (
	pipelineName             = "build-reports"
	gersemiTaskName          = "create-report"
	eirTaskName              = "stats-report"
	versionParamName         = "versions"
	versionArrayLimit        = 10
	edgeReportTaskName       = "edge-upgrade-report"
	edgeReportFromParamName  = "from"
	edgeReportToParamName    = "to"
	maxVersionsForEdgeReport = 3
)

type clients struct {
	Namespace string

	PipelineClient    tknClient.PipelineInterface
	PipelineRunClient tknClient.PipelineRunInterface
}

func (c *clients) k8sClientLogin(namespace string) (*rest.Config, error) {
	// Check KUBECONFIG env var
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		panic(err)
	}

	if namespace != "" {
		c.Namespace = namespace
	} else {
		namespace, _, err := kubeConfig.Namespace()
		if err != nil {
			panic(err)
		}
		c.Namespace = namespace
	}
	return config, err
}

func (c *clients) createTektonClients(config *rest.Config) error {
	cs, err := versioned.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	c.PipelineClient = cs.TektonV1beta1().Pipelines(c.Namespace)
	c.PipelineRunClient = cs.TektonV1beta1().PipelineRuns(c.Namespace)

	return nil
}

func (c *clients) login(namespace string) error {
	cfg, err := c.k8sClientLogin(namespace)
	if err != nil {
		panic(err)
	}
	if c.createTektonClients(cfg) != nil {
		panic(err)
	}
	return nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func (c *clients) buildVersionReports(versions []string) []tkn.PipelineTask {
	taskList := make([]tkn.PipelineTask, 0)

	// Split version list into chunks
	for i := 0; i < len(versions); i += versionArrayLimit {
		batch := versions[i:min(i+versionArrayLimit, len(versions))]
		versionString := strings.Join(batch, " ")

		pipelineTask := tkn.PipelineTask{
			Name: fmt.Sprintf("batch-%d", i),
			TaskRef: &tkn.TaskRef{
				Name: gersemiTaskName,
				Kind: tkn.NamespacedTaskKind,
			},
			Params: []tkn.Param{
				{
					Name: versionParamName,
					Value: tkn.ArrayOrString{
						Type:      tkn.ParamTypeString,
						StringVal: versionString,
					},
				},
			},
		}
		taskList = append(taskList, pipelineTask)
	}
	return taskList
}

func (c *clients) buildEdgeReports(versions []string) []tkn.PipelineTask {
	taskList := make([]tkn.PipelineTask, 0)
	latestVersions := make(map[int64][]string)
	// Find latestVersions versions for each minor we have in the list
	for _, version := range versions {
		v := semver.New(version)
		if versions, ok := latestVersions[v.Minor]; ok {
			latest := versions[len(versions)-1]
			if semver.New(latest).Patch < v.Patch {
				latestVersions[v.Minor] = append(latestVersions[v.Minor], version)
			}
		} else {
			latestVersions[v.Minor] = []string{version}
		}
	}

	for k, v := range latestVersions {
		// Sort in descending order by patch version
		sort.Slice(v, func(i, j int) bool {
			return semver.New(v[i]).Patch > semver.New(v[j]).Patch
		})
		// Leave just last three versions
		if len(v) > 3 {
			latestVersions[k] = v[:3]
		}
	}

	// No two different minor versions found, exit early
	if len(latestVersions) < 1 {
		return taskList
	}

	for i, v := range latestVersions {
		if int(i) == len(latestVersions)-1 {
			// Latest minor release
			continue
		}

		to := strings.Join(v, " ")
		for _, from := range latestVersions[int64(i-1)] {
			undottedFrom := strings.Replace(from, ".", "-", -1)

			pipelineTask := tkn.PipelineTask{
				Name: fmt.Sprintf("upgrade-edge-from-%s", undottedFrom),
				TaskRef: &tkn.TaskRef{
					Name: edgeReportTaskName,
					Kind: tkn.NamespacedTaskKind,
				},
				Params: []tkn.Param{
					{
						Name: edgeReportFromParamName,
						Value: tkn.ArrayOrString{
							Type:      tkn.ParamTypeString,
							StringVal: from,
						},
					},
					{
						Name: edgeReportToParamName,
						Value: tkn.ArrayOrString{
							Type:      tkn.ParamTypeString,
							StringVal: to,
						},
					},
				},
			}
			taskList = append(taskList, pipelineTask)
		}
	}

	return taskList
}

func (c *clients) updatePipeline(versions []string) error {
	taskList := make([]tkn.PipelineTask, 0)
	// Create a list of tasks which builds version reports
	versionTaskList := c.buildVersionReports(versions)
	// After those we'll build latestVersions edge reports
	edgeReportTaskList := c.buildEdgeReports(versions)
	taskList = append(taskList, versionTaskList...)
	taskList = append(taskList, edgeReportTaskList...)
	// Build stats report
	finallyTasks := []tkn.PipelineTask{
		{
			Name: "stats",
			TaskRef: &tkn.TaskRef{
				Name: eirTaskName,
				Kind: tkn.NamespacedTaskKind,
			},
		},
	}

	pipelineSpec := tkn.PipelineSpec{
		Tasks:   taskList,
		Finally: finallyTasks,
	}

	ctx := context.TODO()
	reportPipeline, err := c.PipelineClient.Get(ctx, pipelineName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		reportPipeline := &tkn.Pipeline{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pipelineName,
				Namespace: c.Namespace,
			},
			Spec: pipelineSpec,
		}
		klog.Infof("Creating pipeline %s", pipelineName)
		_, err = c.PipelineClient.Create(ctx, reportPipeline, metav1.CreateOptions{})
		if err != nil {
			panic(err)
		}
	} else {
		reportPipeline.Name = pipelineName
		reportPipeline.Namespace = c.Namespace
		reportPipeline.Spec = pipelineSpec
		klog.Infof("Updating existing pipeline %s", pipelineName)
		_, err = c.PipelineClient.Update(ctx, reportPipeline, metav1.UpdateOptions{})
		if err != nil {
			panic(err)
		}
	}
	return err
}

func (c *clients) createPipelineRun() error {
	pipelineRun := &tkn.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", pipelineName),
			Namespace:    c.Namespace,
		},
		Spec: tkn.PipelineRunSpec{
			PipelineRef: &tkn.PipelineRef{
				Name: pipelineName,
			},
			Timeout: &metav1.Duration{
				Duration: time.Hour,
			},
		},
	}
	ctx := context.TODO()
	pipelineRun, err := c.PipelineRunClient.Create(ctx, pipelineRun, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}
	klog.Infof("Created pipelinerun %s", pipelineRun.Name)
	return err
}

func buildReports(versions []string) error {
	namespace := os.Getenv("PIPELINE_NAMESPACE")

	c := &clients{}
	err := c.login(namespace)
	if err != nil {
		panic(err)
	}

	err = c.updatePipeline(versions)
	if err != nil {
		panic(err)
	}

	return c.createPipelineRun()
}
