package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"

	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"encoding/xml"
)

var netClient *http.Client

var projectKey = ""

var sonarDashStatic = "/dashboard?id="

type (
	Config struct {
		Key                       string
		Name                      string
		Host                      string
		Token                     string
		Version                   string
		Branch                    string
		Sources                   string
		Timeout                   string
		Inclusions                string
		Exclusions                string
		Level                     string
		ShowProfiling             string
		BranchAnalysis            bool
		UsingProperties           bool
		Binaries                  string
		Quality                   string
		QualityEnabled            string
		QualityTimeout            string
		ArtifactFile              string
		JavascitptIcovReport      string
		JavaCoveragePlugin        string
		JacocoReportPath          string
		SSLKeyStorePassword       string
		CacertsLocation           string
		JunitReportPaths          string
		SourceEncoding            string
		SonarTests                string
		JavaTest                  string
		PRKey                     string
		PRBranch                  string
		PRBase                    string
		CoverageExclusion         string
		JavaSource                string
		JavaLibraries             string
		SurefireReportsPath       string
		TypescriptLcovReportPaths string
		Verbose                   string
		CustomJvmParams           string
		TaskId                    string
	}
	// SonarReport it is the representation of .scannerwork/report-task.txt //
	SonarReport struct {
		ProjectKey   string `toml:"projectKey"`
		ServerURL    string `toml:"serverUrl"`
		DashboardURL string `toml:"dashboardUrl"`
		CeTaskID     string `toml:"ceTaskId"`
		CeTaskURL    string `toml:"ceTaskUrl"`
	}
	Plugin struct {
		Config Config
	}
	// TaskResponse Give Compute Engine task details such as type, status, duration and associated component.
	TaskResponse struct {
		Task struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			ComponentID   string `json:"componentId"`
			ComponentKey  string `json:"componentKey"`
			ComponentName string `json:"componentName"`
			AnalysisID    string `json:"analysisId"`
			Status        string `json:"status"`
		} `json:"task"`
	}
	// ProjectStatusResponse Get the quality gate status of a project or a Compute Engine task
	ProjectStatusResponse struct {
		ProjectStatus struct {
			Status string `json:"status"`
		} `json:"projectStatus"`
	}
	Project struct {
		ProjectStatus Status `json:"projectStatus"`
	}
	Status struct {
		Status            string      `json:"status"`
		IgnoredConditions bool        `json:"ignoredConditions"`
		Conditions        []Condition `json:"conditions"`
	}

	Condition struct {
		Status         string `json:"status"`
		MetricKey      string `json:"metricKey"`
		Comparator     string `json:"comparator"`
		PeriodIndex    int    `json:"periodIndex"`
		ErrorThreshold string `json:"errorThreshold"`
		ActualValue    string `json:"actualValue"`
	}

	Testsuites struct {
		XMLName   xml.Name    `xml:"testsuites"`
		Text      string      `xml:",chardata"`
		TestSuite []Testsuite `xml:"testsuite"`
	}
	Testsuite struct {
		Text     string     `xml:",chardata"`
		Package  string     `xml:"package,attr"`
		Time     int        `xml:"time,attr"`
		Tests    int        `xml:"tests,attr"`
		Errors   int        `xml:"errors,attr"`
		Name     string     `xml:"name,attr"`
		TestCase []Testcase `xml:"testcase"`
	}

	Testcase struct {
		Text      string   `xml:",chardata"`
		Time      int      `xml:"time,attr"`      // Actual Value Sonar
		Name      string   `xml:"name,attr"`      // Metric Key
		Classname string   `xml:"classname,attr"` // The metric Rule
		Failure   *Failure `xml:"failure"`        // Sonar Failure - show results
	}
	Failure struct {
		Text    string `xml:",chardata"`
		Message string `xml:"message,attr"`
	}
)

func init() {
	netClient = &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
}

func TryCatch(f func()) func() error {
	return func() (err error) {
		defer func() {
			if panicInfo := recover(); panicInfo != nil {
				err = fmt.Errorf("%v", panicInfo)
				return
			}
		}()
		f() // calling the decorated function
		return err
	}
}
func ParseJunit(projectArray Project, projectName string) Testsuites {
	errors := 0
	total := 0
	testCases := []Testcase{}

	conditionsArray := projectArray.ProjectStatus.Conditions

	for _, condition := range conditionsArray {
		total += 1
		if condition.Status != "OK" {
			errors += 1
			cond := &Testcase{
				Name: condition.MetricKey, Classname: "Violate if " + condition.ActualValue + " is " + condition.Comparator + " " + condition.ErrorThreshold, Failure: &Failure{Message: "Violated: " + condition.ActualValue + " is " + condition.Comparator + " " + condition.ErrorThreshold},
			}
			testCases = append(testCases, *cond)
		} else {
			cond := &Testcase{Name: condition.MetricKey, Classname: "Violate if " + condition.ActualValue + " is " + condition.Comparator + " " + condition.ErrorThreshold, Time: 0}
			testCases = append(testCases, *cond)
		}
	}
	dashboardLink := os.Getenv("PLUGIN_SONAR_HOST") + sonarDashStatic + os.Getenv("PLUGIN_SONAR_NAME")
	SonarJunitReport := &Testsuites{
		TestSuite: []Testsuite{
			Testsuite{
				Time: 13, Package: projectName, Errors: errors, Tests: total, Name: dashboardLink, TestCase: testCases,
			},
		},
	}

	out, _ := xml.MarshalIndent(SonarJunitReport, " ", "  ")
	fmt.Println(string(out))
	fmt.Printf("\n")
	out, _ = xml.MarshalIndent(testCases, " ", "  ")
	fmt.Println(string(out))
	fmt.Printf("\n")

	return *SonarJunitReport
}

func GetProjectKey(key string) string {
	projectKey = strings.Replace(key, "/", ":", -1)
	return projectKey
}
func (p Plugin) Exec() error {

	args := []string{
		"-Dsonar.host.url=" + p.Config.Host,
		"-Dsonar.login=" + p.Config.Token,
	}
	projectFinalKey := p.Config.Key

	if len(p.Config.Verbose) >= 1 {
		args = append(args, "-X")
	}

	if !p.Config.UsingProperties {
		argsParameter := []string{
			"-Dsonar.projectKey=" + projectFinalKey,
			"-Dsonar.projectName=" + p.Config.Name,
			"-Dsonar.projectVersion=" + p.Config.Version,
			"-Dsonar.sources=" + p.Config.Sources,
			"-Dsonar.ws.timeout=" + p.Config.Timeout,
			"-Dsonar.inclusions=" + p.Config.Inclusions,
			"-Dsonar.exclusions=" + p.Config.Exclusions,
			"-Dsonar.log.level=" + p.Config.Level,
			"-Dsonar.showProfiling=" + p.Config.ShowProfiling,
			"-Dsonar.scm.provider=git",
			"-Dsonar.java.binaries=" + p.Config.Binaries,
		}
		args = append(args, argsParameter...)
	}
	if p.Config.BranchAnalysis {
		args = append(args, "-Dsonar.branch.name="+p.Config.Branch)
	}
	if p.Config.QualityEnabled == "true" {
		args = append(args, "-Dsonar.qualitygate.wait="+p.Config.QualityEnabled)
		args = append(args, "-Dsonar.qualitygate.timeout="+p.Config.QualityTimeout)
	}
	if len(p.Config.JavascitptIcovReport) >= 1 {
		args = append(args, "-Dsonar.javascript.lcov.reportPaths="+p.Config.JavascitptIcovReport)
	}
	if len(p.Config.JacocoReportPath) >= 1 {
		args = append(args, "-Dsonar.coverage.jacoco.xmlReportPaths="+p.Config.JacocoReportPath)
		fmt.Printf("\n\n==> Sonar Java Plugin Jacoco configured!\n\n")
		fmt.Printf("\n\n==> -Dsonar.coverage.jacoco.xmlReportPaths=" + p.Config.JacocoReportPath + "\n\n")
	}
	if len(p.Config.JavaCoveragePlugin) >= 1 {
		args = append(args, "-Dsonar.java.coveragePlugin="+p.Config.JavaCoveragePlugin)
		fmt.Printf("\n\n==> Sonar Java Plugin Jacoco Path configured!\n\n")
	}
	if len(p.Config.JunitReportPaths) >= 1 {
		args = append(args, "-Dsonar.junit.reportPaths="+p.Config.JunitReportPaths)
	}
	if len(p.Config.SourceEncoding) >= 1 {
		args = append(args, "-Dsonar.sourceEncoding="+p.Config.SourceEncoding)
	}
	if len(p.Config.SonarTests) >= 1 {
		args = append(args, "-Dsonar.tests="+p.Config.SonarTests)
	}
	if len(p.Config.JavaTest) >= 1 {
		args = append(args, "-Dsonar.java.test.binaries="+p.Config.JavaTest)
	}
	if len(p.Config.CoverageExclusion) >= 1 {
		args = append(args, "-Dsonar.coverage.exclusions="+p.Config.CoverageExclusion)
	}
	if len(p.Config.JavaSource) >= 1 {
		args = append(args, "-Dsonar.java.source="+p.Config.JavaSource)
	}
	if len(p.Config.JavaLibraries) >= 1 {
		args = append(args, "-Dsonar.java.libraries="+p.Config.JavaLibraries)
	}
	if len(p.Config.SurefireReportsPath) >= 1 {
		args = append(args, "-Dsonar.surefire.reportsPath="+p.Config.SurefireReportsPath)
	}
	if len(p.Config.TypescriptLcovReportPaths) >= 1 {
		args = append(args, "-Dsonar.sonar.typescript.lcov.reportPaths="+p.Config.TypescriptLcovReportPaths)
	}
	if len(p.Config.Verbose) >= 1 {
		args = append(args, "-Dsonar.verbose="+p.Config.Verbose)
	}

	if len(p.Config.CustomJvmParams) >= 1 {

		params := strings.Split(p.Config.CustomJvmParams, ",")

		for _, param := range params {
			//fmt.Println(i, param)
			args = append(args, param)
		}

	}

	if len(p.Config.PRKey) >= 1 {
		args = append(args, "-Dsonar.pullrequest.key="+p.Config.PRKey)
	}

	if len(p.Config.PRBranch) >= 1 {
		args = append(args, "-Dsonar.pullrequest.branch="+p.Config.PRBranch)
	}

	if len(p.Config.PRBase) >= 1 {
		args = append(args, "-Dsonar.pullrequest.base="+p.Config.PRBase)
	}

	if len(p.Config.SSLKeyStorePassword) >= 1 {
		args = append(args, "-Djavax.net.ssl.trustStorePassword="+p.Config.SSLKeyStorePassword)
	}

	if len(p.Config.CacertsLocation) >= 1 {
		args = append(args, "-Djavax.net.ssl.trustStore="+p.Config.CacertsLocation)
	}

	os.Setenv("SONAR_USER_HOME", ".sonar")

	fmt.Printf("\n\n")
	fmt.Printf("Starting Plugin - Sonar Scanner Quality Gate Report")
	fmt.Printf("\n")
	fmt.Printf("Developed by Diego Pereira")
	fmt.Printf("\n")
	fmt.Printf("sonar Arguments:")
	fmt.Printf("%v", args)
	fmt.Printf("\n")
	fmt.Printf("\n")

	status := ""

        if p.Config.TaskId != "" {	
		fmt.Printf("Skipping Scan...")
		fmt.Printf("\n")
		fmt.Printf("\n")
		fmt.Printf("#######################################\n")
		fmt.Printf("Waiting for quality gate validation...\n")
		fmt.Printf("#######################################\n")
		status = getStatusID( p.Config)
	} else {
		fmt.Printf("Starting Analisys")
		fmt.Printf("\n")
		cmd := exec.Command("sonar-scanner", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			fmt.Printf("\n\n==> Error in Analysis\n\n")
			fmt.Printf("Error: %s", err.Error())
			//return err
		}
		fmt.Printf("\n==> Sonar Analysis Finished!\n\n")
		fmt.Printf("\n\nStatic Analysis Result:\n\n")
	
		cmd = exec.Command("cat", ".scannerwork/report-task.txt")
	
		cmd.Stdout = os.Stdout
	
		cmd.Stderr = os.Stderr
		fmt.Printf("\n")
		fmt.Printf("#######################################\n")
		fmt.Printf("==> Report Result:\n")
		fmt.Printf("#######################################\n")
		fmt.Printf("\n")
		err = cmd.Run()
	
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("Run command cat reportname failed")
			return err
		}
	
		fmt.Printf("\n\nParsing Results:\n\n")
		fmt.Printf("\n")
		
		report, err := staticScan(&p)
	
		
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("Unable to parse scan results!")
		}
		logrus.WithFields(logrus.Fields{
			"job url": report.CeTaskURL,
		}).Info("Job url")
		fmt.Printf("\n")
		fmt.Printf("\n\nWaiting Analysis to finish:\n\n")
		fmt.Printf("\n")
	
		task, err := waitForSonarJob(report)
	
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Fatal("Unable to get Job state")
			return err
		}
		
	
		fmt.Printf("\n")
		fmt.Printf("#######################################\n")
		fmt.Printf("Waiting for quality gate validation...\n")
		fmt.Printf("#######################################\n")
		fmt.Printf("\n")
	
		status = getStatus(task, report)
	}

	

	fmt.Printf("\n")
	fmt.Printf("==> SONAR PROJECT DASHBOARD <==\n")
	fmt.Printf(p.Config.Host)
	fmt.Printf(sonarDashStatic)
	fmt.Printf(p.Config.Name)
	fmt.Printf("\n==> Harness CIE SonarQube Plugin with Quality Gateway <==\n\n")
	// "Docker", p.Config.ArtifactFile, (p.Config.Host + sonarDashStatic + p.Config.Name), "Sonar", "Harness Sonar Plugin", []string{"Diego", "latest"})

	if status != p.Config.Quality && p.Config.QualityEnabled == "true" {
		fmt.Printf("\n==> QUALITY ENABLED ENALED  - set quality_gate_enabled as false to disable qg\n")
		logrus.WithFields(logrus.Fields{
			"status": status,
		}).Fatal("QualityGate status failed")
	}
	if status != p.Config.Quality && p.Config.QualityEnabled == "false" {
		fmt.Printf("\n==> QUALITY GATEWAY DISABLED\n")
		fmt.Printf("\n==> FAILED <==\n")
		logrus.WithFields(logrus.Fields{
			"status": status,
		}).Info("Quality Gate Status FAILED")
	}
	if status == p.Config.Quality {
		fmt.Printf("\n==> QUALITY GATEWAY ENALED \n")
		fmt.Printf("\n==> PASSED <==\n")
		logrus.WithFields(logrus.Fields{
			"status": status,
		}).Info("Quality Gate Status Success")
	}

	return nil
}

func staticScan(p *Plugin) (*SonarReport, error) {

	cmd := exec.Command("sed", "-e", "s/=/=\"/", "-e", "s/$/\"/", ".scannerwork/report-task.txt")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Run command sed failed")
		return nil, err
	}
	report := SonarReport{}
	err = toml.Unmarshal(output, &report)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Toml Unmarshal failed")
		return nil, err
	}

	return &report, nil
}

func getStatus(task *TaskResponse, report *SonarReport) string {
	reportRequest := url.Values{
		"analysisId": {task.Task.AnalysisID},
	}
	projectRequest, err := http.NewRequest("GET", report.ServerURL+"/api/qualitygates/project_status?"+reportRequest.Encode(), nil)
	projectRequest.Header.Add("Authorization", "Basic "+os.Getenv("TOKEN"))
	projectResponse, err := netClient.Do(projectRequest)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed get status")
	}
	buf, _ := ioutil.ReadAll(projectResponse.Body)
	project := ProjectStatusResponse{}
	if err := json.Unmarshal(buf, &project); err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed")
	}
	fmt.Printf("==> Report Result:\n")
	fmt.Printf(string(buf))

	// JUNUT
	junitReport := ""
	junitReport = string(buf) // returns a string of what was written to it
	fmt.Printf("\n---------------------> JUNIT Exporter <---------------------\n")
	bytesReport := []byte(junitReport)
	var projectReport Project
	err = json.Unmarshal(bytesReport, &projectReport)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v", projectReport)
	fmt.Printf("\n")
	result := ParseJunit(projectReport, "BankingApp")
	file, _ := xml.MarshalIndent(result, "", " ")
	_ = ioutil.WriteFile("sonarResults.xml", file, 0644)

	fmt.Printf("\n")
	fmt.Printf("\n======> JUNIT Exporter <======\n")

	//JUNIT
	fmt.Printf("\n======> Harness Drone/CIE SonarQube Plugin <======\n\n====> Results:")

	return project.ProjectStatus.Status
}

func getStatusID( config *Config) string {
	reportRequest := url.Values{
		"analysisId": {config.TaskId},
	}
	projectRequest, err := http.NewRequest("GET", config.Host+"/api/qualitygates/project_status?"+reportRequest.Encode(), nil)
	projectRequest.Header.Add("Authorization", "Basic "+os.Getenv("TOKEN"))
	projectResponse, err := netClient.Do(projectRequest)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed get status")
	}
	buf, _ := ioutil.ReadAll(projectResponse.Body)
	project := ProjectStatusResponse{}
	if err := json.Unmarshal(buf, &project); err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed")
	}
	fmt.Printf("==> Report Result:\n")
	fmt.Printf(string(buf))

	// JUNUT
	junitReport := ""
	junitReport = string(buf) // returns a string of what was written to it
	fmt.Printf("\n---------------------> JUNIT Exporter <---------------------\n")
	bytesReport := []byte(junitReport)
	var projectReport Project
	err = json.Unmarshal(bytesReport, &projectReport)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v", projectReport)
	fmt.Printf("\n")
	result := ParseJunit(projectReport, "BankingApp")
	file, _ := xml.MarshalIndent(result, "", " ")
	_ = ioutil.WriteFile("sonarResults.xml", file, 0644)

	fmt.Printf("\n")
	fmt.Printf("\n======> JUNIT Exporter <======\n")

	//JUNIT
	fmt.Printf("\n======> Harness Drone/CIE SonarQube Plugin <======\n\n====> Results:")

	return project.ProjectStatus.Status
}

func getSonarJobStatus(report *SonarReport) *TaskResponse {

	taskRequest, err := http.NewRequest("GET", report.CeTaskURL, nil)
	taskRequest.Header.Add("Authorization", "Basic "+os.Getenv("TOKEN"))
	taskResponse, err := netClient.Do(taskRequest)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed get sonar job status")
	}
	buf, _ := ioutil.ReadAll(taskResponse.Body)
	task := TaskResponse{}
	json.Unmarshal(buf, &task)
	return &task
}

func waitForSonarJob(report *SonarReport) (*TaskResponse, error) {
	timeout := time.After(300 * time.Second)
	tick := time.Tick(500 * time.Millisecond)
	for {
		select {
		case <-timeout:
			return nil, errors.New("timed out")
		case <-tick:
			job := getSonarJobStatus(report)
			if job.Task.Status == "SUCCESS" {
				return job, nil
			}
			if job.Task.Status == "ERROR" {
				return nil, errors.New("ERROR")
			}
		}
	}
}
