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

// Package vfio provides server, vfioClient chain elements for the VFIO mechanism connection
package vfio

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"strconv"
	"sync"

	"github.com/golang/protobuf/ptypes/empty"
	"golang.org/x/sys/unix"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
)

const (
	deviceAllowFile    = "devices.allow"
	deviceDenyFile     = "devices.deny"
	deviceStringFormat = "c %v:%v rwm\n"
)

type vfioServer struct {
	vfioDir        string
	cgroupBaseDir  string
	deviceCounters map[string]int
	lock           sync.Mutex
}

// NewServer returns a new VFIO server chain element
func NewServer(vfioDir, cgroupBaseDir string) networkservice.NetworkServiceServer {
	return &vfioServer{
		vfioDir:        vfioDir,
		cgroupBaseDir:  cgroupBaseDir,
		deviceCounters: map[string]int{},
	}
}

func (s *vfioServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (conn *networkservice.Connection, err error) {
	logEntry := log.Entry(ctx).WithField("vfioServer", "Request")

	conn, err = next.Server(ctx).Request(ctx, request)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			// don't forget to call Close to release allocated resources on Endpoint side
			_, errOnClose := next.Server(ctx).Close(ctx, conn)
			if errOnClose != nil {
				logEntry.Error(errOnClose)
			}
		}
	}()

	if mech := vfio.ToMechanism(conn.GetMechanism()); mech != nil {
		var vfioMajor, vfioMinor uint32
		vfioMajor, vfioMinor, err = s.getDeviceNumbers(path.Join(s.vfioDir, vfioDevice))
		if err != nil {
			logEntry.Errorf("failed to get device numbers for the device: %v", vfioDevice)
			return nil, err
		}

		var deviceMajor, deviceMinor uint32
		igid := strconv.FormatUint(uint64(conn.GetContext().GetSriovContext().GetIommuGroup()), 10)
		deviceMajor, deviceMinor, err = s.getDeviceNumbers(path.Join(s.vfioDir, igid))
		if err != nil {
			logEntry.Errorf("failed to get device numbers for the device: %v", igid)
			return nil, err
		}

		cgroupDir := path.Join(s.cgroupBaseDir, mech.GetCgroupDir())

		s.lock.Lock()
		defer s.lock.Unlock()

		if err = s.deviceAllow(cgroupDir, vfioMajor, vfioMinor); err != nil {
			logEntry.Errorf("failed to allow device for the client: %v", vfioDevice)
			return nil, err
		}
		mech.SetVfioMajor(vfioMajor)
		mech.SetVfioMinor(vfioMinor)

		if err = s.deviceAllow(cgroupDir, deviceMajor, deviceMinor); err != nil {
			logEntry.Errorf("failed to allow device for the client: %v", igid)
			_ = s.deviceDeny(cgroupDir, vfioMajor, vfioMinor)
			return nil, err
		}
		mech.SetDeviceMajor(deviceMajor)
		mech.SetDeviceMinor(deviceMinor)
	}

	return conn, nil
}

func (s *vfioServer) getDeviceNumbers(deviceFile string) (major, minor uint32, err error) {
	info := new(unix.Stat_t)
	if err := unix.Stat(deviceFile, info); err != nil {
		return 0, 0, err
	}
	return unix.Major(uint64(info.Rdev)), unix.Minor(uint64(info.Rdev)), nil
}

func (s *vfioServer) deviceAllow(cgroupDir string, major, minor uint32) error {
	key := deviceKey(cgroupDir, major, minor)
	if counter, ok := s.deviceCounters[key]; ok && counter > 0 {
		s.deviceCounters[key] = counter + 1
		return nil
	}

	deviceString := fmt.Sprintf(deviceStringFormat, major, minor)
	if err := ioutil.WriteFile(path.Join(cgroupDir, deviceAllowFile), []byte(deviceString), 0); err != nil {
		return err
	}

	s.deviceCounters[key] = 1

	return nil
}

func (s *vfioServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	logEntry := log.Entry(ctx).WithField("vfioServer", "Close")

	if mech := vfio.ToMechanism(conn.GetMechanism()); mech != nil {
		cgroupDir := path.Join(s.cgroupBaseDir, mech.GetCgroupDir())

		s.lock.Lock()

		vfioMajor := mech.GetVfioMajor()
		vfioMinor := mech.GetVfioMinor()
		if !(vfioMajor == 0 && vfioMinor == 0) {
			if err := s.deviceDeny(cgroupDir, vfioMajor, vfioMinor); err != nil {
				logEntry.Warnf("failed to deny device for the client: %v", vfioDevice)
			}
		}

		deviceMajor := mech.GetDeviceMajor()
		deviceMinor := mech.GetDeviceMinor()
		if !(deviceMajor == 0 && deviceMinor == 0) {
			if err := s.deviceDeny(cgroupDir, deviceMajor, deviceMinor); err != nil {
				logEntry.Warnf("failed to deny device for the client: %v", conn.GetContext().GetSriovContext().GetIommuGroup())
			}
		}

		s.lock.Unlock()
	}

	if _, err := next.Server(ctx).Close(ctx, conn); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

func (s *vfioServer) deviceDeny(cgroupDir string, major, minor uint32) error {
	key := deviceKey(cgroupDir, major, minor)
	s.deviceCounters[key]--
	if s.deviceCounters[key] > 0 {
		return nil
	}

	deviceString := fmt.Sprintf(deviceStringFormat, major, minor)
	return ioutil.WriteFile(path.Join(cgroupDir, deviceDenyFile), []byte(deviceString), 0)
}

func deviceKey(cgroupDir string, major, minor uint32) string {
	return fmt.Sprintf("%s:%d:%d", cgroupDir, major, minor)
}
