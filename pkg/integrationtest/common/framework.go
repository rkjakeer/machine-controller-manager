package common

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	v1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	"github.com/gardener/machine-controller-manager/pkg/integrationtest/common/helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
)

type IntegrationTestFramework struct {
	ControlKubeCluster        *helpers.Cluster
	TargetKubeCluster         *helpers.Cluster
	numberOfBgProcesses       int16
	mcmRepoPath               string
	ctx                       context.Context
	cancelFunc                context.CancelFunc
	wg                        sync.WaitGroup // prevents race condition between main and other goroutines exit
	mcm_logFile               string
	mc_logFile                string
	mcmDeploymentOrigObj      v1.Deployment
	controlClusterNamespace   string
	testMachineClassResources []string
	resourcesTracker          helpers.ResourcesTrackerInterface
}

func NewIntegrationTestFramework(resourcesTracker helpers.ResourcesTrackerInterface) (c *IntegrationTestFramework) {
	c = &IntegrationTestFramework{
		mcmRepoPath:               "../../../dev/mcm",
		mcm_logFile:               filepath.Join(os.TempDir(), "mcm_process.log"),
		mc_logFile:                filepath.Join(os.TempDir(), "mc_process.log"),
		controlClusterNamespace:   os.Getenv("controlClusterNamespace"),
		testMachineClassResources: []string{"test-mc", "test-mc-dummy"},
		numberOfBgProcesses:       0,
		resourcesTracker:          resourcesTracker,
	}
	return c
}

func (c *IntegrationTestFramework) prepareClusters() error {
	/* prepareClusters checks for
	- the validity of controlKubeConfig and targetKubeConfig flags
	- It should return an error if thre is a error
	*/
	controlKubeConfigPath := os.Getenv("controlKubeconfig")
	targetKubeConfigPath := os.Getenv("targetKubeconfig")
	c.ctx, c.cancelFunc = context.WithCancel(context.Background())
	log.Printf("Control cluster kube-config - %s\n", controlKubeConfigPath)
	log.Printf("Target cluster kube-config  - %s\n", targetKubeConfigPath)
	if controlKubeConfigPath != "" {
		controlKubeConfigPath, _ = filepath.Abs(controlKubeConfigPath)
		// if control cluster config is available but not the target, then set control and target clusters as same
		if targetKubeConfigPath == "" {
			targetKubeConfigPath = controlKubeConfigPath
			log.Println("Missing targetKubeConfig. control cluster will be set as target too")
		}
		targetKubeConfigPath, _ = filepath.Abs(targetKubeConfigPath)
		// use the current context in controlkubeconfig
		var err error
		c.ControlKubeCluster, err = helpers.NewCluster(controlKubeConfigPath)
		if err != nil {
			return err
		}
		c.TargetKubeCluster, err = helpers.NewCluster(targetKubeConfigPath)
		if err != nil {
			return err
		}

		// update clientset and check whether the cluster is accessible
		err = c.ControlKubeCluster.FillClientSets()
		if err != nil {
			log.Println("Failed to check nodes in the cluster")
			return err
		}

		err = c.TargetKubeCluster.FillClientSets()
		if err != nil {
			log.Println("Failed to check nodes in the cluster")
			return err
		}
	} else if c.TargetKubeCluster.KubeConfigFilePath != "" {
		return fmt.Errorf("controlKubeconfig path is mandatory if using c.targetKubeConfigPath. Aborting")
	}

	if c.controlClusterNamespace == "" {
		c.controlClusterNamespace = "default"
	}
	if c.ControlKubeCluster.IsSeed(c.TargetKubeCluster) {
		_, err := c.TargetKubeCluster.ClusterName()
		if err != nil {
			log.Println("Failed to determine shoot cluster namespace")
			return err
		}
		c.controlClusterNamespace, _ = c.TargetKubeCluster.ClusterName()
	}
	return nil
}

func (c *IntegrationTestFramework) cloneMcmRepo() error {
	/* clones mcm repo locally.
	This is required if there is no mcm container image tag supplied or
	the clusters are not seed (control) and shoot (target) clusters
	*/
	fi, _ := os.Stat(c.mcmRepoPath)
	if fi.IsDir() {
		log.Printf("skipping as %s directory already exists. If cloning is necessary, delete directory and rerun test", c.mcmRepoPath)
		return nil
	}
	src := "https://github.com/gardener/machine-controller-manager.git"
	if err := helpers.CloningRepo(c.mcmRepoPath, src); err != nil {
		return err
	}
	return nil
}

func (c *IntegrationTestFramework) createCrds() error {
	/* TO-DO: applyCrds will
	- create the custom resources in the controlKubeConfig
	- yaml files are available in kubernetes/crds directory of machine-controller-manager repo
	- resources to be applied are machineclass, machines, machinesets and machinedeployment
	*/

	// err := c.cloneMcmRepo()
	// if err != nil {
	// 	return err
	// }

	applyCrdsDirectory := fmt.Sprintf("%s/kubernetes/crds", c.mcmRepoPath)

	err := c.applyFiles(applyCrdsDirectory)
	if err != nil {
		return err
	}
	return nil
}

func (c *IntegrationTestFramework) startMachineControllerManager(ctx context.Context) error {
	/*
	 startMachineControllerManager starts the machine controller manager
	*/
	command := fmt.Sprintf("make start CONTROL_KUBECONFIG=%s TARGET_KUBECONFIG=%s CONTROL_NAMESPACE=%s LEADER_ELECT=false MACHINE_SAFETY_OVERSHOOTING_PERIOD=300ms", c.ControlKubeCluster.KubeConfigFilePath, c.TargetKubeCluster.KubeConfigFilePath, c.controlClusterNamespace)
	log.Println("starting MachineControllerManager with command: ", command)
	c.wg.Add(1)
	go c.execCommandAsRoutine(ctx, command, c.mcmRepoPath, c.mcm_logFile)
	return nil
}

func (c *IntegrationTestFramework) startMachineController(ctx context.Context) error {
	/*
	  startMachineController starts the machine controller
	*/
	command := fmt.Sprintf("make start CONTROL_KUBECONFIG=%s TARGET_KUBECONFIG=%s CONTROL_NAMESPACE=%s LEADER_ELECT=false", c.ControlKubeCluster.KubeConfigFilePath, c.TargetKubeCluster.KubeConfigFilePath, c.controlClusterNamespace)
	log.Println("starting MachineController with command: ", command)
	c.wg.Add(1)
	go c.execCommandAsRoutine(ctx, command, "../../..", c.mc_logFile)
	return nil
}

func (c *IntegrationTestFramework) prepareMcmDeployment(mcContainerImageTag string, mcmContainerImageTag string, byCreating bool) error {
	/*
		 - if any of mcmContainerImage  or mcContainerImageTag flag is non-empty then,
			 update machinecontrollermanager deployment in the control-cluster with specified image
		 -
	*/
	if byCreating {
		// Create machine-deployment using the yaml file
		err := c.applyFiles("../../../kubernetes/machine-deployment.yaml")
		if err != nil {
			return err
		}
		// once created, the machine-deployment resource container image tags will be updated by continuing here
	}
	providerSpecificRegexp, _ := regexp.Compile("machine-controller-manager-provider-")
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of Deployment before attempting update
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		result, getErr := c.ControlKubeCluster.Clientset.AppsV1().Deployments(c.controlClusterNamespace).Get("machine-controller-manager", metav1.GetOptions{})
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Deployment: %v", getErr))
		}
		c.mcmDeploymentOrigObj = *result
		for i := range result.Spec.Template.Spec.Containers {
			isProviderSpecific := providerSpecificRegexp.Match([]byte(result.Spec.Template.Spec.Containers[i].Name))
			if isProviderSpecific {
				if len(mcContainerImageTag) != 0 {
					result.Spec.Template.Spec.Containers[i].Image = "eu.gcr.io/gardener-project/gardener/machine-controller-manager-provider-aws:" + mcContainerImageTag
				}
			} else {
				if len(mcmContainerImageTag) != 0 {
					result.Spec.Template.Spec.Containers[i].Image = "eu.gcr.io/gardener-project/gardener/machine-controller-manager:" + mcmContainerImageTag
				}
			}
		}
		_, updateErr := c.ControlKubeCluster.Clientset.AppsV1().Deployments(c.controlClusterNamespace).Update(result)
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	} else {
		return nil
	}
}

func (c *IntegrationTestFramework) scaleMcmDeployment(replicas int32) error {
	/*
		 - if any of mcmContainerImage  or mcContainerImageTag flag is non-empty then,
			 update machinecontrollermanager deployment in the control-cluster with specified image
		 -
	*/
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of Deployment before attempting update
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		result, getErr := c.ControlKubeCluster.Clientset.AppsV1().Deployments(c.controlClusterNamespace).Get("machine-controller-manager", metav1.GetOptions{})
		if getErr != nil {
			//panic(fmt.Errorf("failed to get latest version of Deployment: %v", getErr))
			return getErr
		}
		c.mcmDeploymentOrigObj = *result
		*result.Spec.Replicas = replicas
		_, updateErr := c.ControlKubeCluster.Clientset.AppsV1().Deployments(c.controlClusterNamespace).Update(result)
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	} else {
		return nil
	}
}

func (c *IntegrationTestFramework) createMachineClass() error {
	/*
		 - if isControlClusterIsShootsSeed is true, then use machineclass from cluster
			 probe for machine-class in the identified namespace and then creae a copy of this machine-class with additional delta available in machineclass-delta.yaml ( eg. tag (providerSpec.tags)  \"mcm-integration-test: "true"\" )
			  --- (Obsolete ?) ---> the namespace of the new machine-class should be default
	*/

	applyMC := "../../../kubernetes/machine-class.yaml"

	err := c.applyFiles(applyMC)
	if err != nil {
		return err
	}
	return nil
}

func (c *IntegrationTestFramework) createDummyMachineClass() error {
	/* TO-DO: createDummyMachineClass
	 This will read the control cluster machineclass resource and creates a duplicate of it
	 it will additionally add the delta part found in machineclass yaml file

	 - (if not use machine-class.yaml file)
			 look for a file available in kubernetes directory of provider specific repo and then use it instead for creating machine class

	*/

	machineClasses, err := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	var newMachineClass *v1alpha1.MachineClass
	machineClass := machineClasses.Items[0]

	// Create machine-class using yaml and any of existing machineclass resource combined
	for _, resource_name := range c.testMachineClassResources {
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			result, getErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Get(machineClass.GetName(), metav1.GetOptions{})
			if getErr != nil {
				log.Println("Failed to get latest version of machineclass")
				return getErr
			}
			//machineClassOrigObj = *result
			metaData := metav1.ObjectMeta{
				Name:        resource_name,
				Labels:      result.ObjectMeta.Labels,
				Annotations: result.ObjectMeta.Annotations,
			}
			newMachineClass = &v1alpha1.MachineClass{
				ObjectMeta:           metaData,
				ProviderSpec:         result.ProviderSpec,
				SecretRef:            result.SecretRef,
				CredentialsSecretRef: result.CredentialsSecretRef,
				Provider:             result.Provider,
			}
			// c.applyFiles(machineClass)
			// remove dynamic fileds. eg uid, creation time e.t.c.,
			// create result (or machineClassOrigObj) with "../../../kubernetes/machine-class.yaml" content
			_, createErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Create(newMachineClass)
			return createErr
		})
		if retryErr != nil {
			return retryErr
		}

		// patch

		retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// read machineClass patch yaml file ("../../../kubernetes/machine-class-patch.yaml" ) and update machine class(machineClass)
			data, err := os.ReadFile("../../../kubernetes/machine-class-patch.json")
			if err != nil {
				// Error reading file. So skipping it
				return nil
			}
			_, patchErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Patch(newMachineClass.Name, types.MergePatchType, data)
			return patchErr
		})
		if retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func (c *IntegrationTestFramework) applyFiles(filePath string) error {
	var files []string
	err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
		files = append(files, path)
		return nil
	})
	if err != nil {
		panic(err)
	}

	for _, file := range files {
		// log.Println(file)
		fi, err := os.Stat(file)
		if err != nil {
			log.Println("Error: file does not exist!")
			return err
		}

		switch mode := fi.Mode(); {
		case mode.IsDir():
			// do directory stuff
			log.Printf("%s is a directory.\n", file)
		case mode.IsRegular():
			// do file stuff
			err := c.ControlKubeCluster.ApplyYamlFile(file, c.controlClusterNamespace)
			if err != nil {
				//Ignore error if it says the crd already exists
				if !strings.Contains(err.Error(), "already exists") {
					log.Printf("Failed to apply yaml file %s", file)
					//return err
				}
			}
			log.Printf("file %s has been successfully applied to cluster", file)
		}
	}
	return nil
}

func (c *IntegrationTestFramework) execCommandAsRoutine(ctx context.Context, cmd string, dir string, logFile string) {
	c.numberOfBgProcesses++
	args := strings.Fields(cmd)

	command := exec.CommandContext(ctx, args[0], args[1:]...)
	outputFile, err := os.Create(logFile)
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	if err != nil {
		log.Printf("Error occured while creating log file %s. Error is %s", logFile, err)
	}

	defer func() {
		c.numberOfBgProcesses = c.numberOfBgProcesses - 1
		outputFile.Close()
		syscall.Kill(-command.Process.Pid, syscall.SIGINT)
		command.Process.Kill()
		log.Printf("goroutine has been terminated. For more details check %s\n", logFile)
		c.wg.Done()
	}()

	command.Dir = dir
	command.Stdout = outputFile
	command.Stderr = outputFile
	log.Println("command started as a goroutine")
	command.Run()
}

func (c *IntegrationTestFramework) SetupBeforeSuite() {
	/*Check control cluster and target clusters are accessible
	- Check and create crds ( machineclass, machines, machinesets and machinedeployment ) if required
	using file available in kubernetes/crds directory of machine-controller-manager repo
	- Start the Machine Controller manager and machine controller (provider-specific)
	- Assume secret resource for accesing the cloud provider service in already in the control cluster
	- Create machineclass resource from file available in kubernetes directory of provider specific repo in control cluster
	*/
	log.SetOutput(GinkgoWriter)
	mcContainerImageTag := os.Getenv("mcContainerImage")
	mcmContainerImageTag := os.Getenv("mcmContainerImage")

	By("Checking for the clusters if provided are available")
	Expect(c.prepareClusters()).To(BeNil())

	// preparing resources
	if !c.ControlKubeCluster.IsSeed(c.TargetKubeCluster) {

		By("Cloning Machine-Controller-Manager github repo")
		Expect(c.cloneMcmRepo()).To(BeNil())

		By("Applying kubernetes/crds into control cluster")
		Expect(c.createCrds()).To(BeNil())

		By("Applying MachineClass")
		Expect(c.createMachineClass()).To(BeNil())
	} else {
		// If no tags specified then - applyCrds from the mcm repo by cloning
		if !(len(mcContainerImageTag) != 0 && len(mcmContainerImageTag) != 0) {
			By("Cloning Machine-Controller-Manager github repo")
			Expect(c.cloneMcmRepo()).To(BeNil())

			By("Fetching kubernetes/crds and applying them into control cluster")
			Expect(c.createCrds()).To(BeNil())
		}
		By("Creating dup MachineClass with delta yaml")
		Expect(c.createDummyMachineClass()).To(BeNil())
	}

	// starting controllers
	if len(mcContainerImageTag) != 0 && len(mcmContainerImageTag) != 0 {
		log.Println("length is ", len(mcContainerImageTag))
		/* - if any of mcmContainerImage  or mcContainerImageTag flag is non-empty then,
		create/update machinecontrollermanager deployment in the control-cluster with specified image
		- crds already exist in the cluster.
		TO-DO: try to look for crds in local kubernetes directory and apply them. this validates changes in crd structures (if any)
		*/
		if c.ControlKubeCluster.IsSeed(c.TargetKubeCluster) {
			By("Updating MCM Deployemnt")
			Expect(c.prepareMcmDeployment(mcContainerImageTag, mcmContainerImageTag, false)).To(BeNil())
		} else {
			By("Creating MCM Deployemnt")
			Expect(c.prepareMcmDeployment(mcContainerImageTag, mcmContainerImageTag, true)).To(BeNil())
		}

	} else {
		/*
			- as mcmContainerImage is empty, run mc and mcm locally
		*/

		if c.ControlKubeCluster.IsSeed(c.TargetKubeCluster) {
			By("Scaledown existing machine controllers")
			Expect(c.scaleMcmDeployment(0)).To(BeNil())
		}

		By("Starting Machine Controller Manager")
		Expect(c.startMachineControllerManager(c.ctx)).To(BeNil())
		By("Starting Machine Controller")
		Expect(c.startMachineController(c.ctx)).To(BeNil())
	}

	// initialize orphan resource tracker
	machineClass, err := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Get(c.testMachineClassResources[0], metav1.GetOptions{})
	if err == nil {
		secret, err := c.ControlKubeCluster.Clientset.CoreV1().Secrets(machineClass.SecretRef.Namespace).Get(machineClass.SecretRef.Name, metav1.GetOptions{})
		if err == nil {
			clusterName, err := c.ControlKubeCluster.ClusterName()
			Expect(err).NotTo(HaveOccurred())
			err = c.resourcesTracker.InitializeResourcesTracker(machineClass, secret, clusterName)
			//Check there is no error occured
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(err).NotTo(HaveOccurred())
	log.Println("Orphan resource tracker initialized")
}

func (c *IntegrationTestFramework) BeforeEachCheck() {
	BeforeEach(func() {
		if len(os.Getenv("mcContainerImage")) == 0 && len(os.Getenv("mcmContainerImage")) == 0 {
			By("Checking the number of goroutines running are 2")
			Expect(c.numberOfBgProcesses).To(BeEquivalentTo(2))
		}
		// Nodes are healthy
		By("Checking nodes in target cluster are healthy")
		Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 180, 5).Should(BeNumerically("==", c.TargetKubeCluster.NumberOfNodes()))
	})
}

func (c *IntegrationTestFramework) ControllerTests() {
	// Testcase #01 | Machine
	Describe("Machine Resource", func() {
		var initialNodes int16
		Context("Creation", func() {
			// Probe nodes currently available in target cluster
			It("should not lead to any errors", func() {
				// apply machine resource yaml file
				initialNodes = c.TargetKubeCluster.NumberOfNodes()
				By("checking for no errors while creation")
				Expect(c.ControlKubeCluster.ApplyYamlFile("../../../kubernetes/machine.yaml", c.controlClusterNamespace)).To(BeNil())
				//fmt.Println("wait for 30 sec before probing for nodes")
			})
			It("should add 1 more node in target cluster", func() {
				// check whether there is one node more
				By("Waiting until number of ready nodes is 1 more than initial nodes")
				Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 600, 5).Should(BeNumerically("==", initialNodes+1))
			})
		})

		Context("Deletion", func() {
			Context("When machines available", func() {
				It("should not lead to errorsand", func() {
					machinesList, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).List(metav1.ListOptions{})
					if len(machinesList.Items) != 0 {
						By("checking for no errors while deletion")
						Expect(c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).Delete("test-machine", &metav1.DeleteOptions{})).Should(BeNil(), "No Errors while deleting machine")
					}
				})
				It("should remove 1 node in target cluster", func() {
					By("Waiting until number of ready nodes is eual to number of initial  nodes")
					Eventually(c.TargetKubeCluster.NumberOfNodes, 180, 5).Should(BeNumerically("==", initialNodes))
				})
			})
			Context("when machines are not available", func() {
				// delete one machine (non-existent) by random text as name of resource
				// check there are no changes to nodes

				It("should keep nodes intact", func() {
					// Keep count of nodes available
					// delete machine resource
					machinesList, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).List(metav1.ListOptions{})
					if len(machinesList.Items) == 0 {
						err := c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).Delete("test-machine-dummy", &metav1.DeleteOptions{})
						By("checking for errors while deletion")
						Expect(err).To(HaveOccurred())
						time.Sleep(30 * time.Second)
						By("Checking number of ready nodes is eual to number of initial nodes")
						Expect(c.TargetKubeCluster.NumberOfNodes()).To(BeEquivalentTo(initialNodes))
					} else {
						By("Skipping as there are machines available and this check can't be performed")
					}
				})
			})
		})
	})

	// Testcase #02 | Machine Deployment
	Describe("Machine Deployment resource", func() {
		var initialNodes int16
		Context("creation with replicas=3", func() {
			It("should not lead to errors", func() {
				//probe initialnodes before continuing
				initialNodes = c.TargetKubeCluster.NumberOfNodes()

				// apply machinedeployment resource yaml file
				By("checking for no errors while creation")
				Expect(c.ControlKubeCluster.ApplyYamlFile("../../../kubernetes/machine-deployment.yaml", c.controlClusterNamespace)).To(BeNil())
			})
			It("should add 3 more nodes to target cluster", func() {
				// check whether all the expected nodes are ready
				By("Waiting until number of ready nodes are 3 more than initial")
				Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 180, 5).Should(BeNumerically("==", initialNodes+3))
			})
		})
		Context("scale-up with replicas=6", func() {
			It("should not lead to errors", func() {

				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					machineDployment, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Get("test-machine-deployment", metav1.GetOptions{})
					machineDployment.Spec.Replicas = 6
					_, updateErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Update(machineDployment)
					return updateErr
				})
				By("checking for no update errors")
				Expect(retryErr).NotTo(HaveOccurred())
			})
			It("should add futher 3 nodes to target cluster", func() {
				// check whether all the expected nodes are ready
				By("checking number of ready nodes are 6 more than initial")
				Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 180, 5).Should(BeNumerically("==", initialNodes+6))
			})

		})
		Context("scale-down with replicas=2", func() {
			// rapidly scaling back to 2 leading to a freezing and unfreezing
			// check for freezing and unfreezing of machine due to rapid scale up and scale down in the logs of mcm

			It("Should not lead to errors", func() {
				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					machineDployment, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Get("test-machine-deployment", metav1.GetOptions{})
					machineDployment.Spec.Replicas = 2
					_, updateErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Update(machineDployment)
					return updateErr
				})
				By("checking for no update errors")
				Expect(retryErr).NotTo(HaveOccurred())
			})
			It("should remove 4 nodes from target cluster", func() {
				By("checking number of ready nodes are 2 more than initial")
				Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 300, 5).Should(BeNumerically("==", initialNodes+2))
			})
			It("should freeze and unfreeze machineset temporarily", func() {
				By("Reading log file not erroring")
				_, err := os.ReadFile(c.mcm_logFile)
				Expect(err).NotTo(HaveOccurred())

				By("Searching for Froze in mcm log file")
				frozeRegexp, _ := regexp.Compile(` Froze MachineSet`)
				Eventually(func() bool {
					data, _ := ioutil.ReadFile(c.mcm_logFile)
					return frozeRegexp.Match(data)
				}, 300, 5).Should(BeTrue())

				By("Searching Unfroze in mcm log file")
				unfrozeRegexp, _ := regexp.Compile(` Unfroze MachineSet`)
				Eventually(func() bool {
					data, _ := ioutil.ReadFile(c.mcm_logFile)
					return unfrozeRegexp.Match(data)
				}, 300, 5).Should(BeTrue())
			})
		})
		Context("Updation to v2 machine-class and replicas=4", func() {
			// update machine type -> machineDeployment.spec.template.spec.class.name = "test-mc-dummy"
			// scale up replicas by 4
			// To-Do: Add check for rolling update completion (updatedReplicas check)
			It("should upgrade machines to larger machine types and add more nodes to target", func() {
				// wait for 2400s till machines updates
				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					machineDployment, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Get("test-machine-deployment", metav1.GetOptions{})
					machineDployment.Spec.Template.Spec.Class.Name = c.testMachineClassResources[1]
					machineDployment.Spec.Replicas = 4
					_, updateErr := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Update(machineDployment)
					return updateErr
				})
				//Check there is no error occured
				By("checking for no update errors")
				Expect(retryErr).NotTo(HaveOccurred())
				By("updatedReplicas to be 4")
				Eventually(c.ControlKubeCluster.GetUpdatedReplicasCount("test-machine-deployment", c.controlClusterNamespace), 1800, 5).Should(BeNumerically("==", 4))
				By("number of ready nodes be 4 more")
				Eventually(c.TargetKubeCluster.NumberOfReadyNodes, 1800, 5).Should(BeNumerically("==", initialNodes+4))
			})
		})
		Context("Deletion", func() {
			var deploymentReplica int16
			Context("When there are machine deployment(s) available in control cluster", func() {
				It("should not lead to errors and list only initial nodes", func() {
					machineDeployment, err := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Get("test-machine-deployment", metav1.GetOptions{})
					if err == nil {
						// Keep count of nodes available
						deploymentReplica = int16(machineDeployment.Spec.Replicas)
						//delete machine resource
						By("checking for no errors during deletion")
						Expect(c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Delete("test-machine-deployment", &metav1.DeleteOptions{})).Should(BeNil())
						By("Waiting until number of ready nodes is eual to number of initial  nodes")
						Eventually(c.TargetKubeCluster.NumberOfNodes, 300, 5).Should(BeNumerically("==", initialNodes-deploymentReplica))
					}
				})
			})
		})
	})

	// Testcase #03 | Orphaned Resources
	Describe("Zero Orphaned resources check", func() {
		Context("when the hyperscaler resources are querried", func() {
			It("should match with inital resources", func() {
				// if available should delete orphaned resources in cloud provider
				By("Querrying and comparing")
				Expect(c.resourcesTracker.IsOrphanedResourcesAvailable()).To(BeFalse())
			})
		})
	})

}

func (c *IntegrationTestFramework) Cleanup() {

	if len(os.Getenv("mcContainerImage")) == 0 && len(os.Getenv("mcmContainerImage")) == 0 {
		if c.numberOfBgProcesses != 2 {
			c.startMachineController((c.ctx))
			c.startMachineControllerManager((c.ctx))
		}
	}

	if c.ControlKubeCluster.McmClient != nil {
		timeout := int64(900)
		// Check and delete machinedeployment resource
		_, err := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Get("test-machine-deployment", metav1.GetOptions{})
		if err == nil {
			log.Println("deleting test-machine-deployment")
			watchMachinesDepl, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Watch(metav1.ListOptions{TimeoutSeconds: &timeout}) //ResourceVersion: machineDeploymentObj.ResourceVersion
			for event := range watchMachinesDepl.ResultChan() {
				c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineDeployments(c.controlClusterNamespace).Delete("test-machine-deployment", &metav1.DeleteOptions{})
				if event.Type == watch.Deleted {
					watchMachinesDepl.Stop()
					log.Println("machinedeployment deleted")
				}
			}
		} else {
			log.Println(err.Error())
		}
		// Check and delete machine resource
		_, err = c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).Get("test-machine", metav1.GetOptions{})
		if err == nil {
			log.Println("deleting test-machine")
			watchMachines, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).Watch(metav1.ListOptions{TimeoutSeconds: &timeout}) //ResourceVersion: machineObj.ResourceVersion
			for event := range watchMachines.ResultChan() {
				c.ControlKubeCluster.McmClient.MachineV1alpha1().Machines(c.controlClusterNamespace).Delete("test-machine", &metav1.DeleteOptions{})
				if event.Type == watch.Deleted {
					watchMachines.Stop()
					log.Println("machine deleted")
				}
			}
		} else {
			log.Println(err.Error())
		}

		for _, machineClassName := range c.testMachineClassResources {
			// Check and delete machine class resource
			_, err = c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Get(machineClassName, metav1.GetOptions{})
			if err == nil {
				log.Printf("deleting %s machineclass", machineClassName)
				watchMachineClass, _ := c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Watch(metav1.ListOptions{TimeoutSeconds: &timeout}) //ResourceVersion: machineObj.ResourceVersion
				for event := range watchMachineClass.ResultChan() {
					c.ControlKubeCluster.McmClient.MachineV1alpha1().MachineClasses(c.controlClusterNamespace).Delete(machineClassName, &metav1.DeleteOptions{})
					if event.Type == watch.Deleted {
						watchMachineClass.Stop()
						log.Println("machineclass deleted")
					}
				}
			} else {
				log.Println(err.Error())
			}
		}
	}
	if !c.ControlKubeCluster.IsSeed(c.TargetKubeCluster) {
		log.Println("Initiating goroutine cancel via context done")

		c.cancelFunc()

		log.Println("Terminating processes")
		c.wg.Wait()
		log.Println("processes terminated")
	} else {
		retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Retrieve the latest version of Deployment before attempting update
			// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
			_, updateErr := c.ControlKubeCluster.Clientset.AppsV1().Deployments(c.mcmDeploymentOrigObj.Namespace).Update(&(c.mcmDeploymentOrigObj))
			return updateErr
		})
	}
	c.scaleMcmDeployment(1)
}
