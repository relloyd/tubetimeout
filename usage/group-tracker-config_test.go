package usage

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/models"
)

var testFilePath = "/tmp/group_tracker_config.yaml"

func TestGetGroupTrackerConfig_FileNotExist_CreatesFile(t *testing.T) {
	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	defaultGroupTrackerConfigFilePath = testFilePath
	config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(path string) (string, error) {
		return testFilePath, nil
	}

	configFileWritten := false
	config.SafeWriteViaTemp = func(filePath string, data string) error {
		if filePath != testFilePath {
			return errors.New("unexpected file path")
		}
		configFileWritten = true
		return nil
	}

	cfg, err := GetGroupTrackerConfig(tkr)
	assert.NoError(t, err)
	assert.True(t, configFileWritten, "expected file to be written")
	assert.Nil(t, cfg)
}

func TestGetGroupTrackerConfig_FileExists_ParsesYAML(t *testing.T) {
	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	defaultGroupTrackerConfigFilePath = testFilePath
	config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(path string) (string, error) {
		return testFilePath, nil
	}

	data := models.MapGroupTrackerConfig{
		"group1": &models.TrackerConfig{},
	}

	b, _ := yaml.Marshal(data)
	os.WriteFile(testFilePath, b, 0644)

	cfg, err := GetGroupTrackerConfig(tkr)

	assert.NoError(t, err)
	assert.Equal(t, data, cfg)
}

func TestGetGroupTrackerConfig_YAMLError_ReturnsError(t *testing.T) {
	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	defaultGroupTrackerConfigFilePath = testFilePath
	config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(path string) (string, error) {
		return testFilePath, nil
	}

	os.WriteFile(testFilePath, []byte("invalid_yaml"), 0644)
	cfg, err := GetGroupTrackerConfig(tkr)

	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error unmarshalling YAML")
}

func TestSetGroupTrackerConfig_SuccessfulWrite(t *testing.T) {
	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	defaultGroupTrackerConfigFilePath = testFilePath
	config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(path string) (string, error) {
		return testFilePath, nil
	}

	data := models.MapGroupTrackerConfig{
		"group1": &models.TrackerConfig{},
	}

	configFileWritten := false
	config.SafeWriteViaTemp = func(filePath string, content string) error {
		configFileWritten = true
		return nil
	}

	err := SetGroupTrackerConfig(tkr, data)

	assert.NoError(t, err)
	assert.True(t, configFileWritten, "expected file to be written")
}

func TestSetGroupTrackerConfig_EntriesAreFiltered(t *testing.T) {
	existingGroupTrackerConfig := models.MapGroupTrackerConfig{
		"existingGroup": &models.TrackerConfig{Granularity: time.Minute},
	}

	tkr := &Tracker{
		logger:    config.MustGetLogger(),
		mu:        &sync.Mutex{},
		cfgGroups: existingGroupTrackerConfig,
	}

	defaultGroupTrackerConfigFilePath = testFilePath
	config.DefaultCreateAppHomeDirAndGetConfigFilePathFunc = func(path string) (string, error) {
		return testFilePath, nil
	}

	config.SafeWriteViaTemp = func(filePath string, content string) error {
		return nil
	}

	dataThatWillBeFiltered := models.MapGroupTrackerConfig{"": nil, "group": nil}
	err := SetGroupTrackerConfig(tkr, dataThatWillBeFiltered)
	assert.Error(t, err, "expected error due to empty data supplied")
	assert.Equal(t, existingGroupTrackerConfig, tkr.cfgGroups, "expected empty data NOT to be saved")

	expectedGoodData := models.MapGroupTrackerConfig{"abc": &models.TrackerConfig{Granularity: 1 * time.Minute}}
	err = SetGroupTrackerConfig(tkr, expectedGoodData)
	assert.NoError(t, err)
	assert.Equal(t, expectedGoodData, tkr.cfgGroups, "expected group tracker config to be saved")
}
