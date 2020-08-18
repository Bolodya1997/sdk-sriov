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

package pciselector_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/pciselector"
)

const (
	configFileName     = "config.yml"
	hostName           = "service1.example.com"
	clientPf1PciAddr   = "0000:01:00.0"
	clientPf2PciAddr   = "0000:02:00.0"
	endpointPf1PciAddr = "0001:01:00.0"
	endpointPf2PciAddr = "0001:02:00.0"
)

func TestReadConfigFile(t *testing.T) {
	config, err := pciselector.ReadConfig(context.Background(), configFileName)
	assert.Nil(t, err)
	assert.Equal(t, &pciselector.Config{
		Hosts: map[string]map[string]string{
			hostName: {
				clientPf1PciAddr: endpointPf1PciAddr,
				clientPf2PciAddr: endpointPf2PciAddr,
			},
		},
	}, config)
}
