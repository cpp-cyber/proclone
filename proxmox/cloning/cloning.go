package cloning

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cpp-cyber/proclone/auth"
	"github.com/cpp-cyber/proclone/database"
	"github.com/cpp-cyber/proclone/proxmox"
	"github.com/cpp-cyber/proclone/proxmox/cloning/locking"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

const KAMINO_TEMP_POOL string = "0100_Kamino_Templates"
const ROUTER_NAME string = "1-1NAT-pfsense"

var STORAGE_ID string = os.Getenv("STORAGE_ID")

type CloneRequest struct {
	TemplateName string `json:"template_name" binding:"required"`
}

type NewPoolResponse struct {
	Success int `json:"success,omitempty"`
}

type CloneResponse struct {
	Success int      `json:"success"`
	Errors  []string `json:"errors,omitempty"`
}

type NextIDResponse struct {
	Data string `json:"data"`
}

type StorageResponse struct {
	Data []Disk `json:"data"`
}
type Disk struct {
	Id   string `json:"volid"`
	Size int64  `json:"size,omitempty"`
	Used int64  `json:"used,omitempty"`
}

/*
 * ===== CLONE VMS FROM TEMPLATE POOL TO POD POOL =====
 */
func CloneTemplateToPod(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")

	// Make sure user is authenticated
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can access pod data",
		})
		return
	}

	// Parse request body
	var req CloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	usernameStr, ok := username.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid session username"})
		return
	}

	err := ProxmoxCloneTemplate(req.TemplateName, usernameStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pod deployed successfully!"})
}

// assign a user to be a VM user for a resource pool
func setPoolPermission(config *proxmox.ProxmoxConfig, pool string, user string) error {
	// define json data holding new pool name
	jsonString := fmt.Sprintf("{\"path\":\"/pool/%s\", \"users\":\"%s@SDC\", \"roles\":\"PVEVMUser,PVEPoolUser\", \"propagate\": true }", pool, user)
	jsonData := []byte(jsonString)

	statusCode, _, err := proxmox.MakeRequest(config, "api2/json/access/acl", "PUT", jsonData, nil)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("failed to assign pool permissions, status code: %d", statusCode)
	}
	return nil
}

func cleanupClone(config *proxmox.ProxmoxConfig, nodeName string, vmid int) error {
	/*
	 * ----- IF RUNNING, WAIT FOR VM TO BE TURNED OFF -----
	 */
	// assign values to VM struct
	var vm proxmox.VM
	vm.Node = nodeName
	vm.VMID = vmid

	// make request to turn off VM
	_, err := proxmox.StopRequest(config, vm)

	if err != nil {
		// will error if the VM is alr off so just ignore
	}

	// Wait for VM to be "stopped" before continuing
	err = proxmox.WaitForStopped(config, vm)
	if err != nil {
		return fmt.Errorf("stopping vm failed: %v", err)
	}

	/*
	 * ----- HANDLE DELETING VM -----
	 */

	// Prepare request path
	path := fmt.Sprintf("api2/json/nodes/%s/qemu/%d", nodeName, vmid)

	statusCode, body, err := proxmox.MakeRequest(config, path, "DELETE", nil, nil)
	if err != nil {
		return fmt.Errorf("vm delete request failed: %v", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("failed to cleanup VM: %s", string(body))
	}

	return nil
}

// !! Need to refactor to use MakeRequest, idk why I wrote it like this :(
func cloneVM(config *proxmox.ProxmoxConfig, vm proxmox.VirtualResource, newPool string) (newVm *proxmox.VM, err error) {
	// Create a single HTTP client for all requests
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	bestNode, newVMID, err := makeCloneRequest(config, vm, newPool)
	if err != nil {
		return nil, err
	}

	statusURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/status/current",
		config.Host, config.Port, bestNode, newVMID)

	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			if err := cleanupClone(config, vm.NodeName, newVMID); err != nil {
				return nil, fmt.Errorf("clone timed out and cleanup failed: %v", err)
			}
			return nil, fmt.Errorf("clone operation timed out after %v", timeout)
		}

		req, err := http.NewRequest("GET", statusURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create status check request: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to check clone status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Verify the VM is actually cloned
			var statusResponse struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&statusResponse); err != nil {
				return nil, fmt.Errorf("failed to decode status response: %v", err)
			}
			if statusResponse.Data.Status == "running" || statusResponse.Data.Status == "stopped" {
				lockURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/config",
					config.Host, config.Port, bestNode, newVMID)
				lockReq, err := http.NewRequest("GET", lockURL, nil)
				if err != nil {
					return nil, fmt.Errorf("failed to create lock check request: %v", err)
				}
				lockReq.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

				lockResp, err := client.Do(lockReq)
				if err != nil {
					return nil, fmt.Errorf("failed to check lock status: %v", err)
				}
				defer lockResp.Body.Close()

				var configResp struct {
					Data struct {
						Lock string `json:"lock"`
					} `json:"data"`
				}
				if err := json.NewDecoder(lockResp.Body).Decode(&configResp); err != nil {
					return nil, fmt.Errorf("failed to decode lock status: %v", err)
				}
				if configResp.Data.Lock == "" {
					var newVM proxmox.VM
					newVM.VMID = newVMID

					// once node optimization is done must be replaced with new node !!!
					newVM.Node = bestNode

					return &newVM, nil // Clone is complete and VM is not locked
				}
			}
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}
}

func makeCloneRequest(config *proxmox.ProxmoxConfig, vm proxmox.VirtualResource, newPool string) (node string, vmid int, err error) {

	// lock VMID to prevent race conditions

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lock, err := locking.TryAcquireLockWithBackoff(ctx, "lock:vmid", 30*time.Second, 5, 500*time.Millisecond)
	if err != nil {
		return "", 0, fmt.Errorf("failed to acquire vmid lock: %v", err)
	}
	defer lock.Release(ctx)

	// Get next available VMID
	statusCode, body, err := proxmox.MakeRequest(config, "api2/json/cluster/nextid", "GET", nil, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get next VMID: %v", err)
	}

	if statusCode != http.StatusOK {
		return "", 0, fmt.Errorf("failed to get next VMID: %s", string(body))
	}

	var nextID NextIDResponse
	if err := json.Unmarshal(body, &nextID); err != nil {
		return "", 0, fmt.Errorf("failed to decode VMID response: %v", err)
	}

	newVMID, err := strconv.Atoi(nextID.Data)
	if err != nil {
		return "", 0, fmt.Errorf("invalid VMID received: %v", err)
	}

	// find optimal node
	bestNode, err := findBestNode(config)
	if err != nil {
		return "", 0, fmt.Errorf("failed to calculate optimal compute node: %v", err)
	}

	// clone VM
	cloneBody := map[string]interface{}{
		"newid":  newVMID,
		"name":   fmt.Sprintf("%s-clone", vm.Name),
		"pool":   newPool,
		"target": bestNode,
	}

	jsonBody, err := json.Marshal(cloneBody)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request body: %v", err)
	}

	clonePath := fmt.Sprintf("api2/json/nodes/%s/qemu/%d/clone", vm.NodeName, vm.VmId)
	statusCode, body, err = proxmox.MakeRequest(config, clonePath, "POST", jsonBody, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to clone VM: %v", err)
	}
	if statusCode != http.StatusOK {
		return "", 0, fmt.Errorf("failed to clone VM: %s", string(body))
	}

	return bestNode, newVMID, nil
}

// finds lowest available POD ID between 1001 - 1255
func nextPodID(config *proxmox.ProxmoxConfig) (string, int, error) {
	podResponse, err := getAdminPodResponse(config)
	if err != nil {
		return "", 0, fmt.Errorf("failed to fetch pod list from proxmox cluster: %v", err)
	}

	pods := podResponse.Pods
	var ids []int

	// for each pod name, get id from name and append to int array
	for _, pod := range pods {
		id, _ := strconv.Atoi(pod.Name[:4])
		ids = append(ids, id)
	}

	sort.Ints(ids)

	var nextId int
	var gapFound bool = false

	// find first id available starting from 1001
	for i := 1001; i <= 1000+len(ids); i++ {
		nextId = i
		if ids[i-1001] != i {
			gapFound = true
			break
		}
	}

	if !gapFound {
		nextId = 1001 + len(ids)
	}

	// if no ids available between 0 - 255 return error
	if nextId == 1256 {
		err = fmt.Errorf("no pod ids available")
		return "", 0, err
	}

	return strconv.Itoa(nextId), nextId - 1000, nil
}

// ProxmoxCloneTemplate performs the full cloning flow for a given template and user.
func ProxmoxCloneTemplate(templateName, username string) error {
	var errors []string

	templatePool := "kamino_template_" + templateName

	// Load Proxmox configuration
	config, err := proxmox.LoadProxmoxConfig()
	if err != nil {
		return fmt.Errorf("failed to load Proxmox configuration: %v", err)
	}

	// Get all virtual resources
	apiResp, err := proxmox.GetVirtualResources(config)
	if err != nil {
		return fmt.Errorf("failed to fetch virtual resources: %v", err)
	}

	// Find VMs in template pool
	var templateVMs []proxmox.VirtualResource
	var routerTemplate proxmox.VirtualResource
	for _, r := range *apiResp {
		if r.Type == "qemu" && r.ResourcePool == templatePool {
			templateVMs = append(templateVMs, r)
		}
		if r.Name == ROUTER_NAME && r.ResourcePool == KAMINO_TEMP_POOL {
			routerTemplate = r
		}
	}

	if len(templateVMs) == 0 {
		return fmt.Errorf("no VMs found in template pool: %s", templatePool)
	}

	// get next available pod ID
	NewPodID, newPodNumber, err := nextPodID(config)
	if err != nil {
		return fmt.Errorf("failed to get a pod ID: %v", err)
	}

	// create new pod resource pool with ID
	NewPodPool, err := createNewPodPool(username, NewPodID, templateName, config)
	if err != nil {
		return fmt.Errorf("failed to create new pod resource pool: %v", err)
	}

	// Clone 1:1 NAT router from template
	newRouter, err := cloneVM(config, routerTemplate, NewPodPool)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to clone router VM: %v", err))
	}

	// Clone each VM to new pool
	for _, vm := range templateVMs {
		_, err := cloneVM(config, vm, NewPodPool)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to clone VM %s: %v", vm.Name, err))
		}
	}

	// Check if vnet exists, if not, create it
	vnetExists, err := checkForVnet(config, newPodNumber)
	var vnetName string
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to check current vnets: %v", err))
	}

	if !vnetExists {
		vnetName, err = addVNetObject(config, newPodNumber)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to create new vnet object: %v", err))
		}

		err = applySDNChanges(config)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to apply new sdn changes: %v", err))
		}
	} else {
		vnetName = fmt.Sprintf("kamino%d", newPodNumber)
	}

	// Configure VNet of all VMs
	err = setPodVnet(config, NewPodPool, vnetName)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to update pod vnet: %v", err))
	}

	// Turn on router
	if newRouter != nil {
		err = waitForDiskAvailability(config, newRouter.Node, newRouter.VMID, 120*time.Second)
		if err != nil {
			errors = append(errors, fmt.Sprintf("router disk unavailable: %v", err))
		}
		_, err = proxmox.PowerOnRequest(config, *newRouter)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		}

		// Wait for router to be running
		err = proxmox.WaitForRunning(config, *newRouter)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		} else {
			err = configurePodRouter(config, newPodNumber, newRouter.Node, newRouter.VMID)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to configure pod router: %v", err))
			}
		}
	}

	// automatically give user who cloned the pod access
	err = setPoolPermission(config, NewPodPool, username)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to update pool permissions for %s: %v", username, err))
	}

	if len(errors) > 0 {
		// if an error has occured, count # of successfully cloned VMs
		var clonedVMs []proxmox.VirtualResource
		for _, r := range *apiResp {
			if r.Type == "qemu" && r.ResourcePool == NewPodPool {
				clonedVMs = append(clonedVMs, r)
			}
		}

		// if there are no cloned VMs in the resource pool, clean up the resource pool
		if len(clonedVMs) == 0 {
			_ = cleanupFailedPodPool(config, NewPodPool)
		}

		return fmt.Errorf("failed to clone one or more VMs: %v", errors)
	}

	database.AddDeployment(templateName)
	return nil
}

func cleanupFailedPodPool(config *proxmox.ProxmoxConfig, poolName string) error {
	poolDeletePath := fmt.Sprintf("api2/json/pools/%s", poolName)

	statusCode, body, err := proxmox.MakeRequest(config, poolDeletePath, "DELETE", nil, nil)
	if err != nil {
		return fmt.Errorf("pool delete request failed: %v", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("pool delete request failed: %s", string(body))
	}

	return nil
}

func createNewPodPool(username string, newPodID string, templateName string, config *proxmox.ProxmoxConfig) (string, error) {
	newPoolName := newPodID + "_" + templateName + "_" + username

	poolPath := "api2/extjs/pools"

	// define json data holding new pool name
	jsonString := fmt.Sprintf("{\"poolid\":\"%s\"}", newPoolName)
	jsonData := []byte(jsonString)

	_, body, err := proxmox.MakeRequest(config, poolPath, "POST", jsonData, nil)
	if err != nil {
		return "", fmt.Errorf("pool create request failed: %v", err)
	}

	// Parse response
	var newPoolResponse NewPoolResponse
	if err := json.Unmarshal(body, &newPoolResponse); err != nil {
		return "", fmt.Errorf("failed to parse new pool response: %v", err)
	}

	return newPoolName, nil
}

/*
 * /api/admin/proxmox/templates/clone/bulk
 * This function bulk clones a single template for a list of users
 */
func BulkCloneTemplate(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated and is an admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can delete a template",
		})
		return
	}

	var form struct {
		Template string   `json:"template"`
		Names    []string `json:"names"`
	}

	err := c.ShouldBindJSON(&form)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("User %s is cloning %d pods\n", username, len(form.Names))
	eg := errgroup.Group{}
	for i := 0; i < len(form.Names); i++ {
		if form.Names[i] == "" {
			continue
		}
		eg.Go(func() error {
			return ProxmoxCloneTemplate(form.Template, form.Names[i])
		})
	}

	if err := eg.Wait(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pods deployed successfully!"})
}

func waitForDiskAvailability(config *proxmox.ProxmoxConfig, node string, vmid int, maxWait time.Duration) error {
	start := time.Now()
	var status *ConfigResponse
	var err error
	for {
		time.Sleep(2 * time.Second)
		if time.Since(start) > maxWait {
			return fmt.Errorf("timeout waiting for VM disks to become available")
		}

		status, err = getVMConfig(config, node, vmid)
		if err != nil {
			continue
		}

		if status.Data.HardDisk == "" {
			continue
		} else {
			time.Sleep(5 * time.Second)
			return nil
		}

		/*imageId := strings.Split(status.Data.HardDisk, ",")[0]

		disks, err := getStorageContent(config, node, STORAGE_ID)
		if err != nil {
			log.Printf("%v", err)
			continue
		}

		for _, d := range *disks {
			if d.Id == imageId && d.Used > 0 {
				return nil
			}
		} */
	}
}

/*
func getStorageContent(config *proxmox.ProxmoxConfig, node string, storage string) (response *[]Disk, err error) {

	contentPath := fmt.Sprintf("api2/json/nodes/%s/storage/%s/content", node, storage)
	log.Printf("%s", contentPath)

	statusCode, body, err := proxmox.MakeRequest(config, contentPath, "GET", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("%s storage content request failed: %v", node, err)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("storage content request failed: %s", string(body))
	}

	var apiResp StorageResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse storage content response: %v", err)
	}

	return &apiResp.Data, nil
}
*/
