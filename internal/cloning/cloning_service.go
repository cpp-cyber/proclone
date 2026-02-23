package cloning

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
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
	log.Printf("[kayhon] Loaded cloning config: RouterName=%s, RouterVMID=%d, RouterNode=%s, MinPodID=%d, MaxPodID=%d, CloneTimeout=%s, SDNApplyTimeout=%s, RouterWaitTimeout=%s",
		config.RouterName, config.RouterVMID, config.RouterNode, config.MinPodID, config.MaxPodID, config.CloneTimeout, config.SDNApplyTimeout, config.RouterWaitTimeout)
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

	log.Printf("[kayhon] Initializing CloningService with config: RouterName=%s, RouterVMID=%d, RouterNode=%s",
		config.RouterName, config.RouterVMID, config.RouterNode)

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

	log.Printf("[kayhon] === CloneTemplate START === Template=%s, Targets=%d, CheckExisting=%v, StartingVMID=%d",
		req.Template, len(req.Targets), req.CheckExistingDeployments, req.StartingVMID)
	log.Printf("[kayhon] Current config: RouterName=%s, RouterVMID=%d, RouterNode=%s, MinPodID=%d, MaxPodID=%d",
		cs.Config.RouterName, cs.Config.RouterVMID, cs.Config.RouterNode, cs.Config.MinPodID, cs.Config.MaxPodID)

	// 1. Get the template pool and its VMs
	templatePool, err := cs.ProxmoxService.GetPoolVMs("kamino_template_" + req.Template)
	if err != nil {
		log.Printf("[kayhon] Failed to get template pool 'kamino_template_%s': %v", req.Template, err)
		return fmt.Errorf("failed to get template pool: %w", err)
	}
	log.Printf("[kayhon] Step 1: Got template pool 'kamino_template_%s' with %d VMs", req.Template, len(templatePool))
	for _, vm := range templatePool {
		log.Printf("[kayhon]   Template VM: Name=%s, VMID=%d, Node=%s, Status=%s", vm.Name, vm.VmId, vm.NodeName, vm.RunningStatus)
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
	routerPattern := regexp.MustCompile(`(?i)(router|pfsense|vyos)`)

	for _, vm := range templatePool {
		// Check to see if this VM is the router
		if routerPattern.MatchString(vm.Name) {
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
		log.Printf("[kayhon] Step 3: No router found in template pool, using default router: Name=%s, VMID=%d, Node=%s",
			cs.Config.RouterName, cs.Config.RouterVMID, cs.Config.RouterNode)
		router = &proxmox.VM{
			Name: cs.Config.RouterName,
			Node: cs.Config.RouterNode,
			VMID: cs.Config.RouterVMID,
		}
	} else {
		log.Printf("[kayhon] Step 3: Found router in template pool: Name=%s, VMID=%d, Node=%s", router.Name, router.VMID, router.Node)
	}

	// 4. Verify that the pool is not empty
	log.Printf("[kayhon] Step 4: Template has %d non-router VMs", len(templateVMs))
	for _, vm := range templateVMs {
		log.Printf("[kayhon]   Non-router VM: Name=%s, VMID=%d, Node=%s", vm.Name, vm.VMID, vm.Node)
	}
	if len(templateVMs) == 0 {
		log.Printf("[kayhon] ERROR: Template pool %s contains no VMs", req.Template)
		return fmt.Errorf("template pool %s contains no VMs", req.Template)
	}

	// 5. Get pod IDs, Numbers, and VMIDs and assign them to targets
	numVMsPerTarget := len(templateVMs) + 1 // +1 for router
	log.Printf("Number of VMs per target (including router): %d", numVMsPerTarget)

	podIDs, podNumbers, err := cs.ProxmoxService.GetNextPodIDs(cs.Config.MinPodID, cs.Config.MaxPodID, len(req.Targets))
	if err != nil {
		log.Printf("[kayhon] Failed to get next pod IDs (range %d-%d, count %d): %v", cs.Config.MinPodID, cs.Config.MaxPodID, len(req.Targets), err)
		return fmt.Errorf("failed to get next pod IDs: %w", err)
	}
	log.Printf("[kayhon] Step 5: Allocated PodIDs=%v, PodNumbers=%v", podIDs, podNumbers)

	// Lock the vmid allocation mutex to prevent race conditions during vmid allocation
	log.Printf("[kayhon] Acquiring vmidMutex lock for VM ID allocation")
	cs.vmidMutex.Lock()
	log.Printf("[kayhon] vmidMutex lock acquired")

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
			log.Printf("[kayhon] Failed to get next VM IDs (count %d): %v", numVMs, err)
			return fmt.Errorf("failed to get next VM IDs: %w", err)
		}
	}
	log.Printf("[kayhon] Step 5: Allocated VMIDs=%v (total %d VMs)", vmIDs, numVMs)

	for i := range req.Targets {
		req.Targets[i].PoolName = fmt.Sprintf("%s_%s_%s", podIDs[i], req.Template, req.Targets[i].Name)
		req.Targets[i].PodID = podIDs[i]
		req.Targets[i].PodNumber = podNumbers[i]
		req.Targets[i].VMIDs = vmIDs[i*(numVMsPerTarget) : (i+1)*(numVMsPerTarget)]

		log.Printf("Target %s: PodID=%s, PodNumber=%d, VMIDs=%v",
			req.Targets[i].Name, req.Targets[i].PodID, req.Targets[i].PodNumber, req.Targets[i].VMIDs)
	}

	// 6. Create new pool for each target
	log.Printf("[kayhon] Step 6: Creating pools for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		log.Printf("[kayhon] Creating pool: %s (Target=%s, PodID=%s, PodNumber=%d, VMIDs=%v)",
			target.PoolName, target.Name, target.PodID, target.PodNumber, target.VMIDs)
		err = cs.ProxmoxService.CreateNewPool(target.PoolName)
		if err != nil {
			log.Printf("[kayhon] Failed to create pool %s: %v", target.PoolName, err)
			cs.cleanupFailedClones(createdPools)
			return fmt.Errorf("failed to create new pool for %s: %w", target.Name, err)
		}
		createdPools = append(createdPools, target.PoolName)
		log.Printf("[kayhon] Pool created successfully: %s", target.PoolName)
	}

	// 7. Clone targets to proxmox
	req.SSE.Send(
		ProgressMessage{
			Message:  "Cloning VMs",
			Progress: 10,
		},
	)

	log.Printf("[kayhon] Step 7: Cloning VMs for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		// Find best node per target
		bestNode, err := cs.ProxmoxService.FindBestNode()
		if err != nil {
			log.Printf("[kayhon] Failed to find best node for target %s: %v", target.Name, err)
			errors = append(errors, fmt.Sprintf("failed to find best node for %s: %v", target.Name, err))
			continue
		}
		log.Printf("[kayhon] Best node for target %s: %s", target.Name, bestNode)

		// Clone router
		routerCloneReq := proxmox.VMCloneRequest{
			SourceVM:   *router,
			PoolName:   target.PoolName,
			PodID:      target.PodID,
			NewVMID:    target.VMIDs[0],
			TargetNode: bestNode,
		}
		log.Printf("[kayhon] Cloning router for target %s: SourceVM={Name=%s, VMID=%d, Node=%s}, NewVMID=%d, Pool=%s, TargetNode=%s",
			target.Name, routerCloneReq.SourceVM.Name, routerCloneReq.SourceVM.VMID, routerCloneReq.SourceVM.Node,
			routerCloneReq.NewVMID, routerCloneReq.PoolName, routerCloneReq.TargetNode)
		err = cs.ProxmoxService.CloneVM(routerCloneReq)
		if err != nil {
			log.Printf("[kayhon] Failed to clone router VM for %s: %v", target.Name, err)
			errors = append(errors, fmt.Sprintf("failed to clone router VM for %s: %v", target.Name, err))
		} else {
			log.Printf("[kayhon] Router clone initiated successfully for target %s (NewVMID=%d)", target.Name, routerCloneReq.NewVMID)
			// Determine router type
			routerType, err := cs.ProxmoxService.GetRouterType(*router)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to get router type for %s: %v", target.Name, err))
			}

			// Store router info for later operations
			clonedRouters = append(clonedRouters, RouterInfo{
				TargetName: target.Name,
				RouterType: routerType,
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
			log.Printf("[kayhon] Cloning VM %d/%d for target %s: SourceVM={Name=%s, VMID=%d, Node=%s}, NewVMID=%d, Pool=%s, TargetNode=%s",
				i+1, len(templateVMs), target.Name, vm.Name, vm.VMID, vm.Node, vmCloneReq.NewVMID, vmCloneReq.PoolName, vmCloneReq.TargetNode)
			err := cs.ProxmoxService.CloneVM(vmCloneReq)
			if err != nil {
				log.Printf("[kayhon] Failed to clone VM %s for %s: %v", vm.Name, target.Name, err)
				errors = append(errors, fmt.Sprintf("failed to clone VM %s for %s: %v", vm.Name, target.Name, err))
			} else {
				log.Printf("[kayhon] VM clone initiated: %s -> NewVMID=%d for target %s", vm.Name, vmCloneReq.NewVMID, target.Name)
			}
		}
	}

	// 8. Wait for all VM clone operations to complete before configuring VNets
	log.Printf("[kayhon] Step 8: Waiting for clone operations to complete for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		// Wait for all VMs in the pool to be properly cloned
		log.Printf("[kayhon] Waiting for VMs in pool %s to be available (target: %s)", target.PoolName, target.Name)
		time.Sleep(2 * time.Second)

		// First wait for all VMs to appear in the pool
		for retries := range 30 {
			poolVMs, err := cs.ProxmoxService.GetPoolVMs(target.PoolName)
			if err != nil {
				log.Printf("[kayhon] Retry %d/30: Error getting pool VMs for %s: %v", retries+1, target.PoolName, err)
				time.Sleep(2 * time.Second)
				continue
			}

			if len(poolVMs) >= numVMsPerTarget {
				log.Printf("[kayhon] Pool %s has %d VMs (expected %d) - all VMs present", target.PoolName, len(poolVMs), numVMsPerTarget)
				for _, vm := range poolVMs {
					log.Printf("[kayhon]   Pool VM: Name=%s, VMID=%d, Node=%s, Status=%s", vm.Name, vm.VmId, vm.NodeName, vm.RunningStatus)
				}
				break
			}

			log.Printf("[kayhon] Pool %s has %d VMs, waiting for %d (retry %d/30)", target.PoolName, len(poolVMs), numVMsPerTarget, retries+1)
			time.Sleep(2 * time.Second)
		}

		// Wait for all VM locks to be released
		log.Printf("[kayhon] Checking VM locks for pool %s", target.PoolName)
		poolVMs, err := cs.ProxmoxService.GetPoolVMs(target.PoolName)
		if err != nil {
			log.Printf("[kayhon] Failed to get pool VMs after waiting for %s: %v", target.Name, err)
			errors = append(errors, fmt.Sprintf("failed to get pool VMs after waiting for %s: %v", target.Name, err))
			continue
		}

		for _, vm := range poolVMs {
			log.Printf("[kayhon] Waiting for VM %d (%s) lock to be released on node %s", vm.VmId, vm.Name, vm.NodeName)
			if err := cs.ProxmoxService.WaitForLock(vm.NodeName, vm.VmId); err != nil {
				log.Printf("[kayhon] WARNING: Timeout waiting for VM %d (%s) lock: %v", vm.VmId, vm.Name, err)
			} else {
				log.Printf("[kayhon] VM %d (%s) lock released", vm.VmId, vm.Name)
			}
		}

		log.Printf("[kayhon] All clone operations complete for pool %s (target: %s)", target.PoolName, target.Name)
	}

	// Release the vmid allocation mutex now that all of the VMs are cloned on proxmox
	log.Printf("[kayhon] Releasing vmidMutex lock")
	cs.vmidMutex.Unlock()
	log.Printf("[kayhon] vmidMutex lock released")

	// 9. Wait for all router disks to be fully available before configuring VNets.
	// Proxmox clone is two-phase: the clone lock (Phase 1) releases before the storage
	// backend finishes writing the disk (Phase 2). If SetPodVnet runs before Phase 2
	// completes, Proxmox's disk finalization can overwrite the net1 config change,
	// leaving the router connected to the wrong vnet.
	log.Printf("[kayhon] Step 9: Waiting for router disks to be available before configuring VNets (timeout: %s)", cs.Config.RouterWaitTimeout)
	routerDiskReady := make(map[int]bool)
	for _, routerInfo := range clonedRouters {
		log.Printf("[kayhon] Waiting for router disk: Target=%s, VMID=%d, Node=%s, RouterType=%s, PodNumber=%d",
			routerInfo.TargetName, routerInfo.VMID, routerInfo.Node, routerInfo.RouterType, routerInfo.PodNumber)
		if err := cs.ProxmoxService.WaitForDisk(routerInfo.Node, routerInfo.VMID, cs.Config.RouterWaitTimeout); err != nil {
			log.Printf("[kayhon] Router disk unavailable for %s (VMID=%d): %v", routerInfo.TargetName, routerInfo.VMID, err)
			errors = append(errors, fmt.Sprintf("router disk unavailable for %s: %v", routerInfo.TargetName, err))
		} else {
			log.Printf("[kayhon] Router disk ready for %s (VMID=%d)", routerInfo.TargetName, routerInfo.VMID)
			routerDiskReady[routerInfo.VMID] = true
		}
	}

	// 10. Configure VNet of all VMs
	log.Printf("[kayhon] Step 10: Configuring VNets for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		vnetName := fmt.Sprintf("kamino%d", target.PodNumber)
		log.Printf("[kayhon] Setting VNet %s for pool %s (target=%s, PodNumber=%d, VMIDs=%v)",
			vnetName, target.PoolName, target.Name, target.PodNumber, target.VMIDs)
		err = cs.ProxmoxService.SetPodVnet(target.PoolName, vnetName)
		if err != nil {
			log.Printf("[kayhon] Failed to update pod vnet for %s: %v", target.Name, err)
			errors = append(errors, fmt.Sprintf("failed to update pod vnet for %s: %v", target.Name, err))
		} else {
			log.Printf("[kayhon] VNet %s configured successfully for pool %s", vnetName, target.PoolName)
		}
	}

	// 11. Start all routers and wait for them to be running
	req.SSE.Send(
		ProgressMessage{
			Message:  "Starting routers",
			Progress: 25,
		},
	)
	log.Printf("[kayhon] Step 11: Starting %d routers", len(clonedRouters))
	for _, routerInfo := range clonedRouters {
		if !routerDiskReady[routerInfo.VMID] {
			log.Printf("[kayhon] Skipping router start for %s (VMID=%d) - disk not ready", routerInfo.TargetName, routerInfo.VMID)
			continue
		}

		// Start the router
		log.Printf("[kayhon] Starting router VM: Target=%s, VMID=%d, Node=%s, RouterType=%s, PodNumber=%d",
			routerInfo.TargetName, routerInfo.VMID, routerInfo.Node, routerInfo.RouterType, routerInfo.PodNumber)
		err = cs.ProxmoxService.StartVM(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			log.Printf("[kayhon] Failed to start router VM for %s (VMID=%d): %v", routerInfo.TargetName, routerInfo.VMID, err)
			errors = append(errors, fmt.Sprintf("failed to start router VM for %s: %v", routerInfo.TargetName, err))
			continue
		}

		// Wait for router to be running
		log.Printf("[kayhon] Waiting for router VM to reach 'running' status: Target=%s, VMID=%d", routerInfo.TargetName, routerInfo.VMID)
		err = cs.ProxmoxService.WaitForRunning(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			log.Printf("[kayhon] Router VM failed to reach 'running' status for %s (VMID=%d): %v", routerInfo.TargetName, routerInfo.VMID, err)
			errors = append(errors, fmt.Sprintf("failed to start router VM for %s: %v", routerInfo.TargetName, err))
		} else {
			log.Printf("[kayhon] Router VM is running: Target=%s, VMID=%d", routerInfo.TargetName, routerInfo.VMID)
		}
	}

	// 12. Configure all pod routers (separate step after all routers are running)
	req.SSE.Send(
		ProgressMessage{
			Message:  "Configuring pod routers",
			Progress: 33,
		},
	)

	log.Printf("[kayhon] Step 12: Configuring %d pod routers", len(clonedRouters))
	for _, routerInfo := range clonedRouters {
		// Double-check that router is still running before configuration
		log.Printf("[kayhon] Verifying router is running before configuration: Target=%s, VMID=%d, Node=%s",
			routerInfo.TargetName, routerInfo.VMID, routerInfo.Node)
		err = cs.ProxmoxService.WaitForRunning(routerInfo.Node, routerInfo.VMID)
		if err != nil {
			log.Printf("[kayhon] Router not running before configuration for %s (VMID=%d): %v", routerInfo.TargetName, routerInfo.VMID, err)
			errors = append(errors, fmt.Sprintf("router not running before configuration for %s: %v", routerInfo.TargetName, err))
			continue
		}

		log.Printf("[kayhon] Configuring pod router: Target=%s, PodNumber=%d, VMID=%d, Node=%s, RouterType=%s",
			routerInfo.TargetName, routerInfo.PodNumber, routerInfo.VMID, routerInfo.Node, routerInfo.RouterType)
		err = cs.ProxmoxService.ConfigurePodRouter(routerInfo.PodNumber, routerInfo.Node, routerInfo.VMID, routerInfo.RouterType)
		if err != nil {
			log.Printf("[kayhon] Failed to configure pod router for %s (VMID=%d): %v", routerInfo.TargetName, routerInfo.VMID, err)
			errors = append(errors, fmt.Sprintf("failed to configure pod router for %s: %v", routerInfo.TargetName, err))
		} else {
			log.Printf("[kayhon] Pod router configured successfully: Target=%s, VMID=%d, RouterType=%s", routerInfo.TargetName, routerInfo.VMID, routerInfo.RouterType)
		}
	}

	// Router configuration complete - update progress
	req.SSE.Send(
		ProgressMessage{
			Message:  "Finalizing deployment",
			Progress: 90,
		},
	)

	// 12. Set permissions on the pool to the user/group
	log.Printf("[kayhon] Step 13: Setting permissions for %d targets", len(req.Targets))
	for _, target := range req.Targets {
		log.Printf("[kayhon] Setting pool permission: Pool=%s, Target=%s, IsGroup=%v", target.PoolName, target.Name, target.IsGroup)
		err = cs.ProxmoxService.SetPoolPermission(target.PoolName, target.Name, target.IsGroup)
		if err != nil {
			log.Printf("[kayhon] Failed to set pool permissions for %s: %v", target.Name, err)
			errors = append(errors, fmt.Sprintf("failed to update pool permissions for %s: %v", target.Name, err))
		} else {
			log.Printf("[kayhon] Pool permissions set successfully for %s on pool %s", target.Name, target.PoolName)
		}
	}

	// 13. Add deployments to the templates database
	err = cs.DatabaseService.AddDeployment(req.Template, len(req.Targets))
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to increment template deployments for %s: %v", req.Template, err))
	}

	// Final completion message
	req.SSE.Send(
		ProgressMessage{
			Message:  "Template cloning completed!",
			Progress: 100,
		},
	)

	// Handle errors and cleanup if necessary
	if len(errors) > 0 {
		log.Printf("[kayhon] === CloneTemplate COMPLETED WITH ERRORS === Template=%s, ErrorCount=%d", req.Template, len(errors))
		for i, e := range errors {
			log.Printf("[kayhon]   Error %d: %s", i+1, e)
		}
		cs.cleanupFailedClones(createdPools)
		return fmt.Errorf("bulk clone operation completed with errors: %v", errors)
	}

	log.Printf("[kayhon] === CloneTemplate COMPLETED SUCCESSFULLY === Template=%s, Targets=%d", req.Template, len(req.Targets))
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
			}
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
