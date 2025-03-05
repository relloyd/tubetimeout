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

func TestGetGroupTrackerConfig_FileNotExist_CreatesFile(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	_ = os.Remove(testFile.Name()) // remove the file immediately so we have the file name only.

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	configFileWritten := false
	config.FnDefaultSafeWriteViaTemp = func(filePath string, data string) error {
		if filePath != testFile.Name() {
			return errors.New("unexpected file path")
		}
		configFileWritten = true
		return nil
	}

	cfg, err := config.GetConfig[models.MapGroupTrackerConfig](tkr.mu, defaultGroupTrackerConfigFilePath, models.NewMapGroupTrackerConfig)
	assert.NoError(t, err)
	assert.True(t, configFileWritten, "expected file to be written")
	assert.Nil(t, cfg, "expected config to be nil as it was only just written")
}

func TestGetGroupTrackerConfig_FileExists_ParsesYAML(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	t.Cleanup(func() {
		_ = os.Remove(testFile.Name())
	})

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	data := models.MapGroupTrackerConfig{
		"group1": &models.TrackerConfig{},
	}

	b, _ := yaml.Marshal(data)
	err := os.WriteFile(testFile.Name(), b, 0644)
	assert.NoError(t, err)

	cfg, err := config.GetConfig[models.MapGroupTrackerConfig](tkr.mu, defaultGroupTrackerConfigFilePath, models.NewMapGroupTrackerConfig)

	assert.NoError(t, err)
	assert.Equal(t, data, cfg)
}

func TestGetGroupTrackerConfig_YAMLError_ReturnsError(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	t.Cleanup(func() {
		_ = os.Remove(testFile.Name())
	})

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	err := os.WriteFile(testFile.Name(), []byte("invalid_yaml"), 0644)
	assert.NoError(t, err)

	cfg, err := config.GetConfig[models.MapGroupTrackerConfig](tkr.mu, defaultGroupTrackerConfigFilePath, models.NewMapGroupTrackerConfig)

	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error unmarshalling config")
}

func TestSetGroupTrackerConfig_SuccessfulWrite(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	tkr := &Tracker{logger: config.MustGetLogger(), mu: &sync.Mutex{}}

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	t.Cleanup(func() {
		_ = os.Remove(testFile.Name())
	})

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	data := models.MapGroupTrackerConfig{
		"group1": &models.TrackerConfig{},
	}

	configFileWritten := false
	config.FnDefaultSafeWriteViaTemp = func(filePath string, content string) error {
		configFileWritten = true
		return nil
	}

	err := config.SetConfig[models.MapGroupTrackerConfig](tkr.mu, defaultGroupTrackerConfigFilePath, validateGroupTrackerConfig, nil, data)

	assert.NoError(t, err)
	assert.True(t, configFileWritten, "expected file to be written")
}

func TestSetGroupTrackerConfig_EntriesAreFiltered(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	existingGroupTrackerConfig := models.MapGroupTrackerConfig{
		"existingGroup": &models.TrackerConfig{Granularity: time.Minute},
	}

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	t.Cleanup(func() {
		_ = os.Remove(testFile.Name())
	})

	tkr := &Tracker{
		logger:    config.MustGetLogger(),
		mu:        &sync.Mutex{},
		cfgGroups: existingGroupTrackerConfig,
	}

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	config.FnDefaultSafeWriteViaTemp = func(filePath string, content string) error {
		return nil
	}

	dataThatWillBeFiltered := models.MapGroupTrackerConfig{"": nil, "group": nil}
	err := config.SetConfig[models.MapGroupTrackerConfig](tkr.mu, defaultGroupTrackerConfigFilePath, validateGroupTrackerConfig, nil, dataThatWillBeFiltered)
	assert.Error(t, err, "expected error due to empty data supplied")
	assert.Equal(t, existingGroupTrackerConfig, tkr.cfgGroups, "expected empty data NOT to be saved")

	expectedGoodData := models.MapGroupTrackerConfig{"abc": &models.TrackerConfig{Granularity: 1 * time.Minute}}
	err = config.SetConfig[models.MapGroupTrackerConfig](
		tkr.mu,
		defaultGroupTrackerConfigFilePath,
		validateGroupTrackerConfig,
		func(v models.MapGroupTrackerConfig) { tkr.cfgGroups = v },
		expectedGoodData,
	)
	assert.NoError(t, err)
	assert.Equal(t, expectedGoodData, tkr.cfgGroups, "expected group tracker config to be saved")
}

func TestSetGroupTrackerConfig_InvalidGroup(t *testing.T) {
	t.Cleanup(func() {
		restoreFunctions()
	})

	testFile, _ := os.CreateTemp("", "group-tracker-config-*.yaml")
	t.Cleanup(func() {
		_ = os.Remove(testFile.Name())
	})

	existingGroupTrackerConfig := models.MapGroupTrackerConfig{
		"existingGroup": &models.TrackerConfig{Granularity: time.Minute},
	}

	defaultGroupTrackerConfigFilePath = testFile.Name()
	config.FnDefaultCreateAppHomeDirAndGetConfigFilePath = func(path string) (string, error) {
		return testFile.Name(), nil
	}

	config.FnDefaultSafeWriteViaTemp = func(filePath string, content string) error {
		return nil
	}

	tkr := &Tracker{
		logger:    config.MustGetLogger(),
		mu:        &sync.Mutex{},
		cfgGroups: existingGroupTrackerConfig,
	}

	testGroup := "groupName/WithSlash"
	cleanGroup := models.Group(models.NewGroup(testGroup))
	groupData := models.MapGroupTrackerConfig{"groupName/WithSlash": nil}
	err := config.SetConfig[models.MapGroupTrackerConfig](
		tkr.mu,
		defaultGroupTrackerConfigFilePath,
		validateGroupTrackerConfig,
		func(v models.MapGroupTrackerConfig) { tkr.cfgGroups = v },
		groupData,
	)
	assert.NoError(t, err)
	_, ok := tkr.cfgGroups[cleanGroup]
	assert.True(t, ok, "expected clean group to be present")
	_, ok = tkr.cfgGroups[models.Group(testGroup)]
	assert.False(t, ok, "expected test group to be replaced by cleanGroup")
}
