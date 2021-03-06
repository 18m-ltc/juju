// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationmaster_test

import (
	"fmt"
	"time"

	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/migrationmaster"
	"github.com/juju/juju/apiserver/params"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/core/description"
	coremigration "github.com/juju/juju/core/migration"
	"github.com/juju/juju/state"
	coretesting "github.com/juju/juju/testing"
	jujuversion "github.com/juju/juju/version"
)

type Suite struct {
	coretesting.BaseSuite

	model      description.Model
	stub       *testing.Stub
	backend    *stubBackend
	resources  *common.Resources
	authorizer apiservertesting.FakeAuthorizer
}

var _ = gc.Suite(&Suite{})

func (s *Suite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)

	s.model = description.NewModel(description.ModelArgs{
		Config:             map[string]interface{}{"uuid": modelUUID},
		Owner:              names.NewUserTag("admin"),
		LatestToolsVersion: jujuversion.Current,
	})
	s.stub = new(testing.Stub)
	s.backend = &stubBackend{
		migration: &stubMigration{stub: s.stub},
		stub:      s.stub,
		model:     s.model,
	}

	s.resources = common.NewResources()
	s.AddCleanup(func(*gc.C) { s.resources.StopAll() })

	s.authorizer = apiservertesting.FakeAuthorizer{
		EnvironManager: true,
	}
}

func (s *Suite) TestNotEnvironManager(c *gc.C) {
	s.authorizer.EnvironManager = false

	api, err := s.makeAPI()
	c.Assert(api, gc.IsNil)
	c.Assert(err, gc.Equals, common.ErrPerm)
}

func (s *Suite) TestWatch(c *gc.C) {
	api := s.mustMakeAPI(c)

	result := api.Watch()
	c.Assert(result.Error, gc.IsNil)

	resource := s.resources.Get(result.NotifyWatcherId)
	watcher, _ := resource.(state.NotifyWatcher)
	c.Assert(watcher, gc.NotNil)

	select {
	case <-watcher.Changes():
		c.Fatalf("initial event not consumed")
	case <-time.After(coretesting.ShortWait):
	}
}

func (s *Suite) TestGetMigrationStatus(c *gc.C) {
	api := s.mustMakeAPI(c)

	status, err := api.GetMigrationStatus()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(status, gc.DeepEquals, params.MasterMigrationStatus{
		Spec: params.ModelMigrationSpec{
			ModelTag: names.NewModelTag(modelUUID).String(),
			TargetInfo: params.ModelMigrationTargetInfo{
				ControllerTag: names.NewModelTag(controllerUUID).String(),
				Addrs:         []string{"1.1.1.1:1", "2.2.2.2:2"},
				CACert:        "trust me",
				AuthTag:       names.NewUserTag("admin").String(),
				Password:      "secret",
			},
		},
		MigrationId:      "id",
		Phase:            "PRECHECK",
		PhaseChangedTime: s.backend.migration.PhaseChangedTime(),
	})
}

func (s *Suite) TestSetPhase(c *gc.C) {
	api := s.mustMakeAPI(c)

	err := api.SetPhase(params.SetMigrationPhaseArgs{Phase: "ABORT"})
	c.Assert(err, jc.ErrorIsNil)

	c.Assert(s.backend.migration.phaseSet, gc.Equals, coremigration.ABORT)
}

func (s *Suite) TestSetPhaseNoMigration(c *gc.C) {
	s.backend.getErr = errors.New("boom")
	api := s.mustMakeAPI(c)

	err := api.SetPhase(params.SetMigrationPhaseArgs{Phase: "ABORT"})
	c.Assert(err, gc.ErrorMatches, "could not get migration: boom")
}

func (s *Suite) TestSetPhaseBadPhase(c *gc.C) {
	api := s.mustMakeAPI(c)

	err := api.SetPhase(params.SetMigrationPhaseArgs{Phase: "wat"})
	c.Assert(err, gc.ErrorMatches, `invalid phase: "wat"`)
}

func (s *Suite) TestSetPhaseError(c *gc.C) {
	s.backend.migration.setPhaseErr = errors.New("blam")
	api := s.mustMakeAPI(c)

	err := api.SetPhase(params.SetMigrationPhaseArgs{Phase: "ABORT"})
	c.Assert(err, gc.ErrorMatches, "failed to set phase: blam")
}

func (s *Suite) TestSetStatusMessage(c *gc.C) {
	api := s.mustMakeAPI(c)

	err := api.SetStatusMessage(params.SetMigrationStatusMessageArgs{Message: "foo"})
	c.Assert(err, jc.ErrorIsNil)
	c.Check(s.backend.migration.messageSet, gc.Equals, "foo")
}

func (s *Suite) TestSetStatusMessageNoMigration(c *gc.C) {
	s.backend.getErr = errors.New("boom")
	api := s.mustMakeAPI(c)

	err := api.SetStatusMessage(params.SetMigrationStatusMessageArgs{Message: "foo"})
	c.Check(err, gc.ErrorMatches, "could not get migration: boom")
}

func (s *Suite) TestSetStatusMessageError(c *gc.C) {
	s.backend.migration.setMessageErr = errors.New("blam")
	api := s.mustMakeAPI(c)

	err := api.SetStatusMessage(params.SetMigrationStatusMessageArgs{Message: "foo"})
	c.Assert(err, gc.ErrorMatches, "failed to set status message: blam")
}

func (s *Suite) TestExport(c *gc.C) {
	s.model.AddApplication(description.ApplicationArgs{
		Tag:      names.NewApplicationTag("foo"),
		CharmURL: "cs:foo-0",
	})
	const tools = "2.0.0-xenial-amd64"
	m := s.model.AddMachine(description.MachineArgs{Id: names.NewMachineTag("9")})
	m.SetTools(description.AgentToolsArgs{
		Version: version.MustParseBinary(tools),
	})
	api := s.mustMakeAPI(c)

	serialized, err := api.Export()

	c.Assert(err, jc.ErrorIsNil)
	// We don't want to tie this test the serialisation output (that's
	// tested elsewhere). Just check that at least one thing we expect
	// is in the serialised output.
	c.Assert(string(serialized.Bytes), jc.Contains, jujuversion.Current.String())
	c.Assert(serialized.Charms, gc.DeepEquals, []string{"cs:foo-0"})
	c.Assert(serialized.Tools, gc.DeepEquals, []params.SerializedModelTools{
		{tools, "/tools/" + tools},
	})
}

func (s *Suite) TestReap(c *gc.C) {
	api := s.mustMakeAPI(c)

	err := api.Reap()
	c.Check(err, jc.ErrorIsNil)
	s.backend.stub.CheckCalls(c, []testing.StubCall{
		{"RemoveExportingModelDocs", []interface{}{}},
	})
}

func (s *Suite) TestReapError(c *gc.C) {
	s.backend.removeErr = errors.New("boom")
	api := s.mustMakeAPI(c)

	err := api.Reap()
	c.Check(err, gc.ErrorMatches, "boom")
}

func (s *Suite) TestWatchMinionReports(c *gc.C) {
	api := s.mustMakeAPI(c)

	result := api.WatchMinionReports()
	c.Assert(result.Error, gc.IsNil)

	s.stub.CheckCallNames(c,
		"LatestModelMigration",
		"ModelMigration.WatchMinionReports",
	)

	resource := s.resources.Get(result.NotifyWatcherId)
	watcher, _ := resource.(state.NotifyWatcher)
	c.Assert(watcher, gc.NotNil)

	select {
	case <-watcher.Changes():
		c.Fatalf("initial event not consumed")
	case <-time.After(coretesting.ShortWait):
	}
}

func (s *Suite) TestGetMinionReports(c *gc.C) {
	// Report 16 unknowns. These are in reverse order in order to test
	// sorting.
	unknown := make([]names.Tag, 0, 16)
	for i := cap(unknown) - 1; i >= 0; i-- {
		unknown = append(unknown, names.NewMachineTag(fmt.Sprintf("%d", i)))
	}
	m50c0 := names.NewMachineTag("50/lxd/0")
	m50c1 := names.NewMachineTag("50/lxd/1")
	m50 := names.NewMachineTag("50")
	m51 := names.NewMachineTag("51")
	m52 := names.NewMachineTag("52")
	u0 := names.NewUnitTag("foo/0")
	u1 := names.NewUnitTag("foo/1")
	s.backend.migration.minionReports = &state.MinionReports{
		Succeeded: []names.Tag{m50, m51, u0},
		Failed:    []names.Tag{u1, m52, m50c1, m50c0},
		Unknown:   unknown,
	}

	api := s.mustMakeAPI(c)
	reports, err := api.GetMinionReports()
	c.Assert(err, jc.ErrorIsNil)

	// Expect the sample of unknowns to be in order and be limited to
	// the first 10.
	expectedSample := make([]string, 0, 10)
	for i := 0; i < cap(expectedSample); i++ {
		expectedSample = append(expectedSample, names.NewMachineTag(fmt.Sprintf("%d", i)).String())
	}
	c.Assert(reports, gc.DeepEquals, params.MinionReports{
		MigrationId:   "id",
		Phase:         "PRECHECK",
		SuccessCount:  3,
		UnknownCount:  len(unknown),
		UnknownSample: expectedSample,
		Failed: []string{
			// Note sorting
			m50c0.String(),
			m50c1.String(),
			m52.String(),
			u1.String(),
		},
	})
}

func (s *Suite) makeAPI() (*migrationmaster.API, error) {
	return migrationmaster.NewAPI(s.backend, s.resources, s.authorizer)
}

func (s *Suite) mustMakeAPI(c *gc.C) *migrationmaster.API {
	api, err := migrationmaster.NewAPI(s.backend, s.resources, s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
	return api
}

type stubBackend struct {
	migrationmaster.Backend

	stub      *testing.Stub
	getErr    error
	removeErr error
	migration *stubMigration
	model     description.Model
}

func (b *stubBackend) WatchForModelMigration() state.NotifyWatcher {
	b.stub.AddCall("WatchForModelMigration")
	return apiservertesting.NewFakeNotifyWatcher()
}

func (b *stubBackend) LatestModelMigration() (state.ModelMigration, error) {
	b.stub.AddCall("LatestModelMigration")
	if b.getErr != nil {
		return nil, b.getErr
	}
	return b.migration, nil
}

func (b *stubBackend) RemoveExportingModelDocs() error {
	b.stub.AddCall("RemoveExportingModelDocs")
	return b.removeErr
}

func (b *stubBackend) Export() (description.Model, error) {
	b.stub.AddCall("Export")
	return b.model, nil
}

type stubMigration struct {
	state.ModelMigration

	stub          *testing.Stub
	setPhaseErr   error
	phaseSet      coremigration.Phase
	setMessageErr error
	messageSet    string
	minionReports *state.MinionReports
}

func (m *stubMigration) Id() string {
	return "id"
}

func (m *stubMigration) Phase() (coremigration.Phase, error) {
	return coremigration.PRECHECK, nil
}

func (m *stubMigration) PhaseChangedTime() time.Time {
	return time.Date(2016, 6, 22, 16, 38, 0, 0, time.UTC)
}

func (m *stubMigration) Attempt() (int, error) {
	return 1, nil
}

func (m *stubMigration) ModelUUID() string {
	return modelUUID
}

func (m *stubMigration) TargetInfo() (*coremigration.TargetInfo, error) {
	return &coremigration.TargetInfo{
		ControllerTag: names.NewModelTag(controllerUUID),
		Addrs:         []string{"1.1.1.1:1", "2.2.2.2:2"},
		CACert:        "trust me",
		AuthTag:       names.NewUserTag("admin"),
		Password:      "secret",
	}, nil
}

func (m *stubMigration) SetPhase(phase coremigration.Phase) error {
	if m.setPhaseErr != nil {
		return m.setPhaseErr
	}
	m.phaseSet = phase
	return nil
}

func (m *stubMigration) SetStatusMessage(message string) error {
	if m.setMessageErr != nil {
		return m.setMessageErr
	}
	m.messageSet = message
	return nil
}

func (m *stubMigration) WatchMinionReports() (state.NotifyWatcher, error) {
	m.stub.AddCall("ModelMigration.WatchMinionReports")
	return apiservertesting.NewFakeNotifyWatcher(), nil
}

func (m *stubMigration) GetMinionReports() (*state.MinionReports, error) {
	return m.minionReports, nil
}

var modelUUID string
var controllerUUID string

func init() {
	modelUUID = utils.MustNewUUID().String()
	controllerUUID = utils.MustNewUUID().String()
}
