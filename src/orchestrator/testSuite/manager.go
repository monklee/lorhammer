package testSuite

import (
	"lorhammer/src/orchestrator/checker"
	"lorhammer/src/orchestrator/command"
	"lorhammer/src/orchestrator/deploy"
	"lorhammer/src/orchestrator/provisioning"
	"lorhammer/src/orchestrator/testType"
	"lorhammer/src/tools"
	"time"

	"github.com/sirupsen/logrus"
)

var loggerManager = logrus.WithField("logger", "orchestrator/testSuite/test")

//LaunchTest manage life cycle of a test (start, stop, check, report...)
func (test *TestSuite) LaunchTest(consulClient tools.Consul, mqttClient tools.Mqtt, grafanaClient tools.GrafanaClient) (*TestReport, error) {
	check, err := checker.Get(test.Check) //build checker here because no need to start test if checker is bad configured
	if err != nil {
		loggerManager.WithError(err).Error("Error to get checker")
		return nil, err
	}

	if err := deploy.Start(test.Deploy, consulClient); err != nil {
		loggerManager.WithError(err).Error("Error to deploy")
		return nil, err
	}
	startDate := time.Now()

	if err := testType.Start(test.Test, test.Init, mqttClient); err != nil {
		loggerManager.WithError(err).Error("Error to start test")
		return nil, err
	}

	// wait until stop (0 or negative value means no stop)
	time.Sleep(test.StopAllLorhammerTime)
	if test.StopAllLorhammerTime > 0 {
		command.StopScenario(mqttClient)
	}

	//wait until check minus time we have already passed in stop
	time.Sleep(test.SleepBeforeCheckTime - test.StopAllLorhammerTime)
	success, errors := checkResults(check)

	//wait until shutdown minus time we have already passed in stop and check (0 or negative value means no shutdown)
	time.Sleep(test.ShutdownAllLorhammerTime - (test.StopAllLorhammerTime + test.SleepBeforeCheckTime))

	if test.StopAllLorhammerTime > 0 || test.ShutdownAllLorhammerTime > 0 {
		if err := provisioning.DeProvision(test.UUID); err != nil {
			loggerManager.WithError(err).Error("Couldn't unprovision")
			return nil, err
		}
	}

	if test.ShutdownAllLorhammerTime > 0 {
		command.ShutdownLorhammers(mqttClient)
	}
	endDate := time.Now()
	var snapshotURL = ""
	// TODO add time for grafana snapshot (idem stop and shutdown)
	if grafanaClient != nil {
		var err error
		snapshotURL, err = grafanaClient.MakeSnapshot(startDate, endDate)
		if err != nil {
			loggerManager.WithError(err).Error("Can't snapshot grafana")
		}
	}
	return &TestReport{
		StartDate:          startDate,
		EndDate:            endDate,
		Input:              test,
		ChecksSuccess:      success,
		ChecksError:        errors,
		GrafanaSnapshotURL: snapshotURL,
	}, nil
}

func checkResults(check checker.Checker) ([]checker.Success, []checker.Error) {
	ok, errs := check.Check()
	if len(errs) > 0 {
		loggerManager.WithField("nb", len(errs)).Error("Check results errors")
		for _, err := range errs {
			loggerManager.WithFields(logrus.Fields(err.Details())).Error("Check result error")
		}
	}

	if len(ok) > 0 {
		loggerManager.WithField("nb", len(ok)).Info("Check results good")
		for _, o := range ok {
			loggerManager.WithFields(logrus.Fields(o.Details())).Info("Check result good")
		}
	}

	return ok, errs
}
