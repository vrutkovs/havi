package main

import (
	"io/ioutil"
	"os"
	"path"

	git "github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v2"
	"k8s.io/klog"
)

type Channel struct {
	Name     string   `yaml:"name"`
	Versions []string `yaml:"versions"`
}

const (
	clonePath       = "/tmp/graph-data"
	channelsDirName = "channels"
)

func getChannelNames(clonePath string) ([]string, error) {
	channelsDirPath := path.Join(clonePath, channelsDirName)
	channelsDirFile, err := os.Open(channelsDirPath)
	if err != nil {
		klog.Fatalf("failed opening directory: %s", err)
	}
	defer channelsDirFile.Close()

	return channelsDirFile.Readdirnames(0) // 0 to read all files and folders
}

func getVersionsFromChannel(channelName string) ([]string, error) {
	channelFilePath := path.Join(clonePath, channelsDirName, channelName)
	channelsFile, err := os.Open(channelFilePath)
	if err != nil {
		return nil, err
	}
	defer channelsFile.Close()
	byteValue, _ := ioutil.ReadAll(channelsFile)

	var channel Channel

	yaml.Unmarshal(byteValue, &channel)
	return channel.Versions, nil
}

func cloneGraphData() error {

	os.RemoveAll(clonePath)
	_, err := git.PlainClone(clonePath, false, &git.CloneOptions{
		URL:          "https://github.com/openshift/cincinnati-graph-data",
		SingleBranch: true,
		Depth:        1,
	})
	return err
}
