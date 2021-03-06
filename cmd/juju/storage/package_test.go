// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage_test

import (
	"testing"

	"github.com/juju/cmd"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	jujutesting "github.com/juju/juju/testing"
)

func TestAll(t *testing.T) {
	gc.TestingT(t)
}

type BaseStorageSuite struct {
	jujutesting.FakeJujuXDGDataHomeSuite

	command cmd.Command
}

func (s *BaseStorageSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
}

func (s *BaseStorageSuite) TearDownTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.TearDownTest(c)
}

type SubStorageSuite struct {
	jujutesting.FakeJujuXDGDataHomeSuite
	store *jujuclienttesting.MemStore
}

func (s *SubStorageSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)

	s.store = jujuclienttesting.NewMemStore()
	s.store.CurrentControllerName = "testing"
	s.store.Controllers["testing"] = jujuclient.ControllerDetails{}
	s.store.Accounts["testing"] = jujuclient.AccountDetails{
		User: "admin@local",
	}
}
