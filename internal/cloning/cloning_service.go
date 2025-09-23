package cloning

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/kelseyhightower/envconfig"
)

// LoadCloningConfig loads and validates cloning configuration from environment variables
func LoadCloningConfig() (*Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return nil, fmt.Errorf("failed to process cloning configuration: %w", err)
	}
	return &config, nil
}

func NewTemplateClient(db *sql.DB) *TemplateClient {
	return &TemplateClient{
		DB: db,
		TemplateConfig: &TemplateConfig{
			UploadDir: os.Getenv("UPLOAD_DIR"),
		},
	}
}

func NewDatabaseService(db *sql.DB) DatabaseService {
	return NewTemplateClient(db)
}

func (c *TemplateClient) GetTemplateConfig() *TemplateConfig {
	return c.TemplateConfig
}

func NewCloningService(proxmoxService proxmox.Service, db *sql.DB, ldapService ldap.Service) (*CloningService, error) {
	config, err := LoadCloningConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load cloning configuration: %w", err)
	}

	if config.RouterVMID == 0 || config.RouterNode == "" {
		return nil, fmt.Errorf("incomplete cloning configuration")
	}

	return &CloningService{
		ProxmoxService:  proxmoxService,
		DatabaseService: NewDatabaseService(db),
		LDAPService:     ldapService,
		Config:          config,
	}, nil
}

func (cs *CloningService) CloneTemplate(req CloneRequest) error {
	var errors []string
	var createdPools []string
	var clonedRouters []RouterInfo

	// 1. Get the template pool and its VMs
	templatePool, err := cs.ProxmoxService.GetPoolVMs("kamino_template_" + req.Template)
	if err != nil {
		return fmt.Errorf("failed to get template pool: %w", err)
	}

	// 2. Check if any template is already deployed (if requested)
	if req.CheckExistingDeployments {
		for _, target := range req.Targets {
			targetPoolName := fmt.Sprintf("%s_%s", req.Template, target.Name)
			isValid, err := cs.ValidateCloneRequest(targetPoolName, target.Name)
			if err != nil {
				return fmt.Errorf("failed to validate the deployment of template for %s: %w", target.Name, err)
			}
			if !isValid {
				return fmt.Errorf("template %s is already deployed for %s or they have exceeded the maximum of 5 deployed pods", req.Template, target.Name)
			}
		}
	}

	// 3. Identify router and other VMs
	var router *proxmox.VM
	var templateVMs []proxmox.VM

	for _, vm := range templatePool {
		// Check to see if this VM is the router
		lowerVMName := strings.ToLower(vm.Name)
		if strings.Contains(lowerVMName, "router") || strings.Contains(lowerVMName, "pfsense") {
			router = &proxmox.VM{
				Name: vm.Name,
				Node: vm.NodeName,
				VMID: vm.VmId,
			}
		} else {
			templateVMs = append(templateVMs, proxmox.VM{
				Name: vm.Name,
				Node: vm.NodeName,
				VMID: vm.VmId,
			})
		}
	}

	// If no router was found in the template, use the default router template
	if router == nil {
		router = &proxmox.VM{
			Name: cs.Config.RouterName,
			Node: cs.Config.RouterNode,
			VMID: cs.Config.RouterVMID,
		}
	}

	// 4. Verify that the pool is not empty
	if len(templateVMs) == 0 {
		return fmt.Errorf("template pool %s contains no VMs", req.Template)
	}

	// 5. Get pod IDs, Numbers, and VMIDs and assign them to targets
	numVMsPerTarget := len(templateVMs) + 1 // +1 for router
	log.Printf("Number of VMs per target (including router): %d", numVMsPerTarget)

	podIDs, podNumbers, err := cs.ProxmoxService.GetNextPodIDs(cs.Config.MinPodID, cs.Config.MaxPodID, len(req.Targets))
	if err != nil {
		return fmt.Errorf("failed to get next pod IDs: %w", err)
	}

	// Lock the vmid allocation mutex to prevent race conditions during vmid allocation
	cs.vmidMutex.Lock()

	// Use StartingVMID from request if provided, otherwise get next available VMIDs
	var vmIDs []int
	numVMs := len(req.Targets) * numVMsPerTarget
	if req.StartingVMID != 0 {
		log.Printf("Starting VMID allocation from specified starting VMID: %d", req.StartingVMID)
		for i := range numVMs {
			vmIDs = append(vmIDs, req.StartingVMID+i)
		}
	} else {
		vmIDs, err = cs.ProxmoxService.GetNextVMIDs(numVMs)
		if err != nil {
			return fmt.Errorf("failed to get next VM IDs: %w", err)
		}
	}

	for i := range req.Targets {
		req.Targets[i].PoolName = fmt.Sprintf("%s_%s_%s", podIDs[i], req.Template, req.Targets[i].Name)
		req.Targets[i].PodID = podIDs[i]
		req.Targets[i].PodNumber = podNumbers[i]
		req.Targets[i].VMIDs = vmIDs[i*(numVMsPerTarget) : (i+1)*(numVMsPerTarget)]

		log.Printf("Target %s: PodID=%s, PodNumber=%d, VMIDs=%v",
			req.Targets[i].Name, req.Targets[i].PodID, req.Targets[i].PodNumber, req.Targets[i].VMIDs)
	}

	// 6. Create new pool for each target
	for _, target := range req.Targets {
		err = cs.ProxmoxService.CreateNewPool(target.PoolName)
		if err != nil {
			cs.cleanupFailedClones(createdPools)
			return fmt.Errorf("failed to create new pool for %s: %w", target.Name, err)
		}
		createdPools = append(createdPools, target.PoolName)
	}

	// 7. Clone targets to proxmox
	for _, target := range req.Targets {
		// Find best node per target
		bestNode, err := cs.ProxmoxService.FindBestNode()
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to find best node for %s: %v", target.Name, err))
			continue
		}

		// Clone router
		routerCloneReq := proxmox.VMCloneRequest{
			SourceVM:   *router,
			PoolName:   target.PoolName,
			PodID:      target.PodID,
			NewVMID:    target.VMIDs[0],
			TargetNode: bestNode,
		}
		err = cs.ProxmoxService.CloneVM(routerCloneReq)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to clone router VM for %s: %v", target.Name, err))
		} else {
			// Store router info for later operations
			clonedRouters = append(clonedRouters, RouterInfo{
				TargetName: target.Name,
				PodNumber:  target.PodNumber,
				Node:       bestNode,
				VMID:       target.VMIDs[0],
			})
		}

		// Clone each VM to new pool
		for i, vm := range templateVMs {
			vmCloneReq := proxmox.VMCloneRequest{
				SourceVM:   vm,
				PoolName:   target.PoolName,
				PodID:      target.PodID,
				NewVMID:    target.VMIDs[i+1],
				TargetNode: bestNode,
			}
			err := cs.ProxmoxService.CloneVM(vmCloneReq)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to clone VM %s for %s: %v", vm.Name, target.Name, err))
			}
		}
	}

	// 8. Wait for all VM clone operations to complete before configuring VNets
	log.Printf("Waiting for clone operations to complete for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		// Wait for all VMs in the pool to be properly cloned
		log.Printf("Waiting for VMs in pool %s to be available", target.PoolName)
		time.Sleep(2 * time.Second)

		// Check if pool has the expected number of VMs
		for retries := range 30 {
			poolVMs, err := cs.ProxmoxService.GetPoolVMs(target.PoolName)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			if len(poolVMs) >= numVMsPerTarget {
				log.Printf("Pool %s has %d VMs (expected %d) - clone operations complete", target.PoolName, len(poolVMs), numVMsPerTarget)
				break
			}

			log.Printf("Pool %s has %d VMs, waiting for %d (retry %d/30)", target.PoolName, len(poolVMs), numVMsPerTarget, retries+1)
			time.Sleep(2 * time.Second)
		}
	}

	// Release the vmid allocation mutex now that all of the VMs are cloned on proxmox
	cs.vmidMutex.Unlock()

	// 9. Configure VNet of all VMs
	log.Printf("Configuring VNets for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		vnetName := fmt.Sprintf("kamino%d", target.PodNumber)
		log.Printf("Setting VNet %s for pool %s (target: %s)", vnetName, target.PoolName, target.Name)
		err = cs.SetPodVnet(target.PoolName, vnetName)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to update pod vnet for %s: %v", target.Name, err))
		}
	}

	// 10. Start all routers and wait for them to be running
	log.Printf("Starting %d routers", len(clonedRouters))
	for _, routerInfo := range clonedRouters {
		// Wait for router disk to be available
		log.Printf("Waiting for router disk to be available for %s (VMID: %d)", routerInfo.TargetName, routerInfo.VMID)
		err = cs.ProxmoxService.WaitForDisk(routerInfo.Node, routerInfo.VMID, cs.Config.RouterWaitTimeout)
		if err != nil {
			errors = append(errors, fmt.Sprintf("router disk unavailable for %s: %v", routerInfo.TargetName, err))
			continue
		}

		// Start the router
		log.Printf("Starting router VM for %s (VMID: %d)", routerInfo.TargetName, routerInfo.VMID)
		err = cs.ProxmoxService.StartVM(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM for %s: %v", routerInfo.TargetName, err))
			continue
		}

		// Wait for router to be running
		log.Printf("Waiting for router VM to be running for %s (VMID: %d)", routerInfo.TargetName, routerInfo.VMID)
		err = cs.ProxmoxService.WaitForRunning(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM for %s: %v", routerInfo.TargetName, err))
		}
	}

	// 11. Configure all pod routers (separate step after all routers are running)
	log.Printf("Configuring %d pod routers", len(clonedRouters))
	for _, routerInfo := range clonedRouters {
		// Double-check that router is still running before configuration
		err = cs.ProxmoxService.WaitForRunning(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("router not running before configuration for %s: %v", routerInfo.TargetName, err))
			continue
		}

		log.Printf("Configuring pod router for %s (Pod: %d, VMID: %d)", routerInfo.TargetName, routerInfo.PodNumber, routerInfo.VMID)
		err = cs.configurePodRouter(routerInfo.PodNumber, routerInfo.Node, routerInfo.VMID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to configure pod router for %s: %v", routerInfo.TargetName, err))
		}
	}

	// 12. Set permissions on the pool to the user/group
	for _, target := range req.Targets {
		err = cs.ProxmoxService.SetPoolPermission(target.PoolName, target.Name, target.IsGroup)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to update pool permissions for %s: %v", target.Name, err))
		}
	}

	// 13. Add deployments to the templates database
	err = cs.DatabaseService.AddDeployment(req.Template, len(req.Targets))
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to increment template deployments for %s: %v", req.Template, err))
	}

	// Handle errors and cleanup if necessary
	if len(errors) > 0 {
		cs.cleanupFailedClones(createdPools)
		return fmt.Errorf("bulk clone operation completed with errors: %v", errors)
	}

	return nil
}

func (cs *CloningService) DeletePod(pod string) error {

	// 1. Check if pool is already empty
	isEmpty, err := cs.ProxmoxService.IsPoolEmpty(pod)
	if err != nil {
		return fmt.Errorf("failed to check if pool %s is empty: %w", pod, err)
	}

	if isEmpty {
		if err := cs.ProxmoxService.DeletePool(pod); err != nil {
			return fmt.Errorf("failed to delete empty pool %s: %w", pod, err)
		}
		return nil
	}

	// 2. Get all virtual machines in the pool
	poolVMs, err := cs.ProxmoxService.GetPoolVMs(pod)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs for %s: %w", pod, err)
	}

	// 3. Stop all VMs and wait for them to be stopped
	var runningVMs []proxmox.VM
	stoppedCount := 0

	for _, vm := range poolVMs {
		if vm.Type == "qemu" {
			// Only stop if VM is running
			if vm.RunningStatus == "running" {
				err := cs.ProxmoxService.StopVM(vm.NodeName, vm.VmId)
				if err != nil {
					return fmt.Errorf("failed to stop VM %s: %w", vm.Name, err)
				}

				// Only add to wait list if it was actually running
				runningVMs = append(runningVMs, proxmox.VM{
					Node: vm.NodeName,
					VMID: vm.VmId,
				})
				stoppedCount++
			} else {
			}
		} else {
		}
	}

	// Wait for all previously running VMs to be stopped
	if len(runningVMs) > 0 {
		for _, vm := range runningVMs {
			if err := cs.ProxmoxService.WaitForStopped(vm.Node, vm.VMID); err != nil {
				// Continue with deletion even if we can't confirm the VM is stopped
			}
		}
	}

	// 4. Delete all VMs
	deletedCount := 0

	for _, vm := range poolVMs {
		if vm.Type == "qemu" {
			err := cs.ProxmoxService.DeleteVM(vm.NodeName, vm.VmId)
			if err != nil {
				return fmt.Errorf("failed to delete VM %s: %w", vm.Name, err)
			}
			deletedCount++
		}
	}

	// 5. Wait for all VMs to be deleted and pool to become empty
	err = cs.ProxmoxService.WaitForPoolEmpty(pod, 5*time.Minute)
	if err != nil {
		// Continue with pool deletion even if we can't confirm all VMs are gone
	}

	// 6. Delete the pool
	err = cs.ProxmoxService.DeletePool(pod)
	if err != nil {
		return fmt.Errorf("failed to delete pool %s: %w", pod, err)
	}

	return nil
}

func (cs *CloningService) cleanupFailedClones(createdPools []string) {
	for _, poolName := range createdPools {
		// Check if pool has any VMs
		poolVMs, err := cs.ProxmoxService.GetPoolVMs(poolName)
		if err != nil {
			continue // Skip if we can't check
		}

		// If pool is empty, delete it
		if len(poolVMs) == 0 {
			_ = cs.ProxmoxService.DeletePool(poolName)
		}
	}
}
