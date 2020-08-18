// Copyright (c) 2020 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pciselector

import (
	"context"
	"io/ioutil"
	"path/filepath"

	"github.com/ghodss/yaml"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/pkg/errors"
)

// Config contains list of Client -> Endpoint physical functions mappings for each host
type Config struct {
	Hosts map[string]map[string]string `yaml:"hosts"`
}

// ReadConfig reads configuration from file
func ReadConfig(ctx context.Context, configFile string) (*Config, error) {
	logEntry := log.Entry(ctx).WithField("Config", "ReadConfig")

	yamlConfig, err := ioutil.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return nil, errors.Wrapf(err, "error reading file: %v", configFile)
	}

	config := &Config{}
	if err = yaml.Unmarshal(yamlConfig, config); err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling yaml: %s", yamlConfig)
	}

	logEntry.Infof("yaml Config: %s", yamlConfig)
	logEntry.Infof("unmarshalled Config: %+v", config)

	return config, nil
}
