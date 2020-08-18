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

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/networkservicemesh/sdk-sriov/pkg/sriov"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/pciselector"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/resourcepool"
	"github.com/networkservicemesh/sdk-sriov/pkg/tools/yamlhelper"
)

const (
	hostInfoFileName    = "host_info.yml"
	endpointVf11PciAddr = "0001:01:00.1"
	endpointVf21PciAddr = "0001:02:00.1"
	endpointVf22PciAddr = "0001:02:00.2"
	capability          = "10G"
)

func testHostInfo() *sriov.HostInfo {
	host := &sriov.HostInfo{}
	_ = yamlhelper.UnmarshalFile(hostInfoFileName, host)
	return host
}

func TestSelector_Select(t *testing.T) {
	rp := &resourcePoolMock{}

	config, err := pciselector.ReadConfig(context.TODO(), configFileName)
	assert.Nil(t, err)

	s := pciselector.NewSelector(context.TODO(), rp, config)

	err = s.Update(hostName, testHostInfo())
	assert.Nil(t, err)

	// 1. Select with the least capability

	rp.mock.On("SelectAny", endpointPf1PciAddr, sriov.KernelDriver).Once().
		Return(endpointVf11PciAddr, nil)

	selection, err := s.Select(testHostInfo(), sriov.VfioPCIDriver, sriov.KernelDriver, capability)
	assert.Nil(t, err)
	assert.Equal(t, endpointVf11PciAddr, selection.EndpointVfPciAddress)
	assert.Equal(t, clientPf1PciAddr, selection.ClientPfPciAddress)
	assert.Equal(t, uint(2), selection.ClientIommuGroup)

	// 2. Select from the already selected IOMMU group

	rp.mock.On("SelectAny", endpointPf1PciAddr, sriov.KernelDriver).
		Return("", errors.New("no VF"))
	rp.mock.On("SelectAny", endpointPf2PciAddr, sriov.KernelDriver).Once().
		Return(endpointVf21PciAddr, nil)

	selection, err = s.Select(testHostInfo(), sriov.VfioPCIDriver, sriov.KernelDriver, capability)
	assert.Nil(t, err)
	assert.Equal(t, endpointVf21PciAddr, selection.EndpointVfPciAddress)
	assert.Equal(t, clientPf2PciAddr, selection.ClientPfPciAddress)
	assert.Equal(t, uint(2), selection.ClientIommuGroup)

	// 3. Select the last and then try to select again

	rp.mock.On("SelectAny", endpointPf2PciAddr, sriov.KernelDriver).Once().
		Return(endpointVf22PciAddr, nil)

	selection, err = s.Select(testHostInfo(), sriov.VfioPCIDriver, sriov.KernelDriver, capability)
	assert.Nil(t, err)
	assert.Equal(t, endpointVf22PciAddr, selection.EndpointVfPciAddress)
	assert.Equal(t, clientPf2PciAddr, selection.ClientPfPciAddress)
	assert.Equal(t, uint(1), selection.ClientIommuGroup)

	rp.mock.On("SelectAny", endpointPf2PciAddr, sriov.KernelDriver).
		Return("", errors.New("no VF"))

	_, err = s.Select(testHostInfo(), sriov.VfioPCIDriver, sriov.KernelDriver, capability)
	assert.NotNil(t, err)
}

func TestSelector_Free(t *testing.T) {
	rp := &resourcePoolMock{}

	config, err := pciselector.ReadConfig(context.TODO(), configFileName)
	assert.Nil(t, err)

	s := pciselector.NewSelector(context.TODO(), rp, config)

	err = s.Update(hostName, testHostInfo())
	assert.Nil(t, err)

	rp.mock.On("SelectAny", endpointPf1PciAddr, sriov.KernelDriver).
		Return(endpointVf11PciAddr, nil)
	rp.mock.On("Free", endpointVf11PciAddr).
		Return()

	selection, err := s.Select(testHostInfo(), sriov.VfioPCIDriver, sriov.KernelDriver, capability)
	assert.Nil(t, err)
	assert.Equal(t, endpointVf11PciAddr, selection.EndpointVfPciAddress)
	assert.Equal(t, clientPf1PciAddr, selection.ClientPfPciAddress)
	assert.Equal(t, uint(2), selection.ClientIommuGroup)

	s.Free(hostName, selection)
	rp.mock.AssertCalled(t, "Free", endpointVf11PciAddr)

	selection, err = s.Select(testHostInfo(), sriov.KernelDriver, sriov.KernelDriver, capability)
	assert.Nil(t, err)
	assert.Equal(t, endpointVf11PciAddr, selection.EndpointVfPciAddress)
	assert.Equal(t, clientPf1PciAddr, selection.ClientPfPciAddress)
	assert.Equal(t, uint(2), selection.ClientIommuGroup)
}

type resourcePoolMock struct {
	mock mock.Mock

	resourcepool.ResourcePool
}

func (r *resourcePoolMock) SelectAny(pfPciAddr string, driverType sriov.DriverType) (string, error) {
	res := r.mock.Called(pfPciAddr, driverType)
	return res.String(0), res.Error(1)
}

func (r *resourcePoolMock) Free(vfPciAddr string) {
	r.mock.Called(vfPciAddr)
}
