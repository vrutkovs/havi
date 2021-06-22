package main

import (
	"strings"

	"github.com/coreos/go-semver/semver"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
)

const minimalSupportedMinor = 5

func main() {
	klog.InitFlags(nil)
	klog.Info("Cloning graph-data repo")

	err := cloneGraphData()
	if err != nil {
		panic(err)
	}

	channels, err := getChannelNames(clonePath)
	if err != nil {
		panic(err)
	}

	versions := sets.NewString()
	for _, channel := range channels {
		channelVersions, err := getVersionsFromChannel(channel)
		// Drop versions containing arch suffix - we don't use these anymore
		channelVersions = filter(channelVersions, func(version string) bool {
			return !strings.Contains(version, "+")
		})
		// Drop versions less than supportedMinorVersion
		channelVersions = filter(channelVersions, func(version string) bool {
			v := semver.New(version)
			return v.Minor >= minimalSupportedMinor
		})

		if err != nil {
			// TODO: add channel name in warning
			klog.Warning(err)
			continue
		}
		klog.Infof("Found %d versions from %s channel", len(channelVersions), channel)
		versions.Insert(channelVersions...)
	}

	klog.Infof("Discovered %d unique versions", len(versions))

	err = buildReports(versions.List())
	if err != nil {
		panic(err)
	}
}

func filter(vs []string, f func(string) bool) []string {
	filtered := make([]string, 0)
	for _, v := range vs {
		if f(v) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
