package cloning

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/auth"
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

// NewTemplateClient creates a new template client
func NewTemplateClient(db *sql.DB) *TemplateClient {
	return &TemplateClient{
		DB: db,
		TemplateConfig: &TemplateConfig{
			UploadDir: os.Getenv("UPLOAD_DIR"),
		},
	}
}

// NewDatabaseService creates a new database service
func NewDatabaseService(db *sql.DB) DatabaseService {
	return NewTemplateClient(db)
}

// GetTemplateConfig returns the template configuration
func (c *TemplateClient) GetTemplateConfig() *TemplateConfig {
	return c.TemplateConfig
}

// NewTemplateClient creates a new template client
func NewCloningManager(proxmoxService proxmox.Service, db *sql.DB, ldapService auth.Service) (*CloningManager, error) {
	config, err := LoadCloningConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load cloning configuration: %w", err)
	}

	if config.Realm == "" || config.RouterVMID == 0 || config.RouterNode == "" {
		return nil, fmt.Errorf("incomplete cloning configuration")
	}

	return &CloningManager{
		ProxmoxService:  proxmoxService,
		DatabaseService: NewDatabaseService(db),
		LDAPService:     ldapService,
		Config:          config,
	}, nil
}

// CloneTemplate clones a template pool for a user or group
func (cm *CloningManager) CloneTemplate(template string, targetName string, isGroup bool) error {
	log.Printf("Starting cloning process for template '%s' to target '%s' (isGroup: %t)", template, targetName, isGroup)
	var errors []string

	// 1. Get the template pool and its VMs
	log.Printf("Step 1: Retrieving VMs from template pool '%s'", template)
	templatePool, err := cm.ProxmoxService.GetPoolVMs(template)
	if err != nil {
		log.Printf("ERROR: Failed to get template pool '%s': %v", template, err)
		return fmt.Errorf("failed to get template pool: %w", err)
	}
	log.Printf("Successfully retrieved %d VMs from template pool '%s'", len(templatePool), template)

	// 2. Check if the template has already been cloned by the user

	// Extract template name from pool name (remove kamino_template_ prefix)
	log.Printf("Step 2: Checking deployment status for template")
	templateName := strings.TrimPrefix(template, "kamino_template_")
	log.Printf("Extracted template name: '%s' from pool '%s'", templateName, template)

	targetPoolName := fmt.Sprintf("%s_%s", templateName, targetName)
	log.Printf("Checking if target pool '%s' is already deployed", targetPoolName)
	isDeployed, err := cm.IsDeployed(targetPoolName)
	if err != nil {
		log.Printf("ERROR: Failed to check deployment status for '%s': %v", targetPoolName, err)
		return fmt.Errorf("failed to check if template is deployed: %w", err)
	}

	if isDeployed {
		log.Printf("ERROR: Template '%s' is already deployed for target '%s'", template, targetName)
		return fmt.Errorf("template %s is already or in the process of being deployed %s", template, targetName)
	}
	log.Printf("Template is not deployed, proceeding with cloning")

	// 3. Identify router and other VMs
	log.Printf("Step 3: Analyzing template VMs to identify router and other VMs")
	var router *proxmox.VM
	var templateVMs []proxmox.VM

	for _, vm := range templatePool {
		// Check to see if this VM is the router
		lowerVMName := strings.ToLower(vm.Name)
		log.Printf("Analyzing VM: '%s' (Type: %s)", vm.Name, vm.Type)
		if strings.Contains(lowerVMName, "router") || strings.Contains(lowerVMName, "pfsense") {
			log.Printf("Identified VM '%s' as router", vm.Name)
			router = &proxmox.VM{
				Name: vm.Name,
				Node: vm.NodeName,
				VMID: vm.VmId,
			}
		} else {
			log.Printf("Added VM '%s' to template VMs list", vm.Name)
			templateVMs = append(templateVMs, proxmox.VM{
				Name: vm.Name,
				Node: vm.NodeName,
				VMID: vm.VmId,
			})
		}
	}

	if router != nil {
		log.Printf("Router VM identified: '%s' (VMID: %d, Node: %s)", router.Name, router.VMID, router.Node)
	} else {
		log.Printf("No router VM found in template, will use default router")
	}
	log.Printf("Template contains %d non-router VMs to clone", len(templateVMs))

	// 4. Verify that the pool is not empty
	if len(templateVMs) == 0 {
		log.Printf("ERROR: Template pool '%s' contains no VMs", template)
		return fmt.Errorf("template pool %s contains no VMs", template)
	}
	log.Printf("Template validation passed: %d VMs ready for cloning", len(templateVMs))

	// 5. Get the next available pod ID and create new pool
	log.Printf("Step 5: Allocating pod ID and creating new pool")
	newPodID, newPodNumber, err := cm.ProxmoxService.GetNextPodID(cm.Config.MinPodID, cm.Config.MaxPodID)
	if err != nil {
		log.Printf("ERROR: Failed to get next pod ID: %v", err)
		return fmt.Errorf("failed to get next pod ID: %w", err)
	}
	log.Printf("Allocated pod ID: %s (Pod number: %d)", newPodID, newPodNumber)

	newPoolName := fmt.Sprintf("%s_%s_%s", newPodID, templateName, targetName)
	log.Printf("Creating new pool: '%s'", newPoolName)

	err = cm.ProxmoxService.CreateNewPool(newPoolName)
	if err != nil {
		log.Printf("ERROR: Failed to create pool '%s': %v", newPoolName, err)
		return fmt.Errorf("failed to create new pool: %w", err)
	}
	log.Printf("Successfully created new pool: '%s'", newPoolName)

	// 6. Clone the router and all VMs
	log.Printf("Step 6: Starting VM cloning process")

	// If no router was found in the template, use the default router template
	if router == nil {
		router = &proxmox.VM{
			Name: cm.Config.RouterName,
			Node: cm.Config.RouterNode,
			VMID: cm.Config.RouterVMID,
		}
		log.Printf("Using default router template: '%s' (VMID: %d)", router.Name, router.VMID)
	}

	log.Printf("Cloning router VM '%s' to pool '%s'", router.Name, newPoolName)
	newRouter, err := cm.ProxmoxService.CloneVM(*router, newPoolName)
	if err != nil {
		log.Printf("ERROR: Failed to clone router VM '%s': %v", router.Name, err)
		errors = append(errors, fmt.Sprintf("failed to clone router VM: %v", err))
	} else {
		log.Printf("Successfully cloned router VM: '%s' -> new VMID: %d", router.Name, newRouter.VMID)
	}

	// Clone each VM to new pool
	log.Printf("Cloning %d template VMs to pool '%s'", len(templateVMs), newPoolName)
	for i, vm := range templateVMs {
		log.Printf("Cloning VM %d/%d: '%s' (VMID: %d)", i+1, len(templateVMs), vm.Name, vm.VMID)
		clonedVM, err := cm.ProxmoxService.CloneVM(vm, newPoolName)
		if err != nil {
			log.Printf("ERROR: Failed to clone VM '%s': %v", vm.Name, err)
			errors = append(errors, fmt.Sprintf("failed to clone VM %s: %v", vm.Name, err))
		} else {
			log.Printf("Successfully cloned VM: '%s' -> new VMID: %d", vm.Name, clonedVM.VMID)
		}
	}
	log.Printf("VM cloning phase completed")

	var vnetName = fmt.Sprintf("kamino%d", newPodNumber)

	// 7. Configure VNet of all VMs
	log.Printf("Step 7: Configuring VNet for all VMs in pool '%s'", newPoolName)
	log.Printf("Setting VNet to: '%s' for pod number %d", vnetName, newPodNumber)
	err = cm.SetPodVnet(newPoolName, vnetName)
	if err != nil {
		log.Printf("ERROR: Failed to configure VNet '%s' for pool '%s': %v", vnetName, newPoolName, err)
		errors = append(errors, fmt.Sprintf("failed to update pod vnet: %v", err))
	} else {
		log.Printf("Successfully configured VNet '%s' for pool '%s'", vnetName, newPoolName)
	}

	// 8. Turn on Router
	log.Printf("Step 8: Starting and configuring router VM")
	if newRouter != nil {
		log.Printf("Waiting for router disk availability (VMID: %d, Node: %s, Timeout: %v)",
			newRouter.VMID, newRouter.Node, cm.Config.RouterWaitTimeout)
		err = cm.ProxmoxService.WaitForDisk(newRouter.Node, newRouter.VMID, cm.Config.RouterWaitTimeout)
		if err != nil {
			log.Printf("ERROR: Router disk unavailable (VMID: %d): %v", newRouter.VMID, err)
			errors = append(errors, fmt.Sprintf("router disk unavailable: %v", err))
		} else {
			log.Printf("Router disk is available, starting VM (VMID: %d)", newRouter.VMID)
		}

		log.Printf("Starting router VM (VMID: %d, Node: %s)", newRouter.VMID, newRouter.Node)
		err = cm.ProxmoxService.StartVM(newRouter.Node, newRouter.VMID)
		if err != nil {
			log.Printf("ERROR: Failed to start router VM (VMID: %d): %v", newRouter.VMID, err)
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		} else {
			log.Printf("Successfully started router VM (VMID: %d)", newRouter.VMID)
		}

		// 9. Wait for router to be running
		log.Printf("Step 9: Waiting for router to be fully running")
		err = cm.ProxmoxService.WaitForRunning(*newRouter)
		if err != nil {
			log.Printf("ERROR: Router failed to reach running state (VMID: %d): %v", newRouter.VMID, err)
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		} else {
			log.Printf("Router is now running, proceeding with configuration (Pod: %d, VMID: %d)", newPodNumber, newRouter.VMID)
			err = cm.configurePodRouter(newPodNumber, newRouter.Node, newRouter.VMID)
			if err != nil {
				log.Printf("ERROR: Failed to configure pod router (Pod: %d, VMID: %d): %v", newPodNumber, newRouter.VMID, err)
				errors = append(errors, fmt.Sprintf("failed to configure pod router: %v", err))
			} else {
				log.Printf("Successfully configured pod router (Pod: %d, VMID: %d)", newPodNumber, newRouter.VMID)
			}
		}
	} else {
		log.Printf("WARNING: No router VM available to start")
	}

	// 10. Set permissions on the pool to the user/group
	log.Printf("Step 10: Setting permissions on pool '%s' for target '%s' (isGroup: %t)", newPoolName, targetName, isGroup)
	err = cm.ProxmoxService.SetPoolPermission(newPoolName, targetName, cm.Config.Realm, isGroup)
	if err != nil {
		log.Printf("ERROR: Failed to set permissions on pool '%s' for '%s': %v", newPoolName, targetName, err)
		errors = append(errors, fmt.Sprintf("failed to update pool permissions for %s: %v", targetName, err))
	} else {
		log.Printf("Successfully set permissions on pool '%s' for '%s'", newPoolName, targetName)
	}

	// 11. Add a +1 to the total deployments in the templates database
	log.Printf("Step 11: Updating deployment counter for template '%s'", templateName)
	err = cm.DatabaseService.AddDeployment(templateName)
	if err != nil {
		log.Printf("ERROR: Failed to increment deployment counter for template '%s': %v", templateName, err)
		errors = append(errors, fmt.Sprintf("failed to increment template deployments for %s: %v", templateName, err))
	} else {
		log.Printf("Successfully incremented deployment counter for template '%s'", templateName)
	}

	// If there were any errors, clean up if necessary
	if len(errors) > 0 {
		log.Printf("Cloning completed with %d errors, checking cleanup requirements", len(errors))
		log.Printf("Errors encountered: %v", errors)

		// Check if any VMs were successfully cloned
		clonedVMs, checkErr := cm.ProxmoxService.GetPoolVMs(newPoolName)
		if checkErr != nil {
			log.Printf("WARNING: Could not check cloned VMs for cleanup: %v", checkErr)
		} else {
			log.Printf("Found %d VMs in pool '%s' after failed cloning", len(clonedVMs), newPoolName)
		}

		if len(clonedVMs) == 0 {
			log.Printf("No VMs were successfully cloned, deleting empty pool '%s'", newPoolName)
			deleteErr := cm.ProxmoxService.DeletePool(newPoolName)
			if deleteErr != nil {
				log.Printf("WARNING: Failed to cleanup empty pool '%s': %v", newPoolName, deleteErr)
			} else {
				log.Printf("Successfully cleaned up empty pool '%s'", newPoolName)
			}
		} else {
			log.Printf("Some VMs were cloned successfully, leaving pool '%s' for manual cleanup", newPoolName)
		}

		return fmt.Errorf("clone operation completed with errors: %v", errors)
	}

	log.Printf("Successfully cloned pool '%s' to '%s' for '%s'", template, newPoolName, targetName)
	log.Printf("Cloning process completed successfully - Pool: '%s', VMs: %d, Target: '%s'",
		newPoolName, len(templateVMs)+1, targetName) // +1 for router
	return nil
}

// DeletePod deletes a pod pool for a user or group
func (cm *CloningManager) DeletePod(pod string) error {
	log.Printf("Starting deletion process for pod '%s'", pod)

	// 1. Check if pool is already empty
	log.Printf("Step 1: Checking if pool '%s' is empty", pod)
	isEmpty, err := cm.ProxmoxService.IsPoolEmpty(pod)
	if err != nil {
		log.Printf("ERROR: Failed to check if pool '%s' is empty: %v", pod, err)
		return fmt.Errorf("failed to check if pool %s is empty: %w", pod, err)
	}

	if isEmpty {
		log.Printf("Pool '%s' is already empty, proceeding directly to pool deletion", pod)
		err := cm.ProxmoxService.DeletePool(pod)
		if err != nil {
			log.Printf("ERROR: Failed to delete empty pool '%s': %v", pod, err)
		} else {
			log.Printf("Successfully deleted empty pool '%s'", pod)
		}
		return err
	}

	// 2. Get all virtual machines in the pool
	log.Printf("Step 2: Retrieving all VMs from pool '%s'", pod)
	poolVMs, err := cm.ProxmoxService.GetPoolVMs(pod)
	if err != nil {
		log.Printf("ERROR: Failed to get VMs from pool '%s': %v", pod, err)
		return fmt.Errorf("failed to get pool VMs for %s: %w", pod, err)
	}

	log.Printf("Found %d VMs in pool '%s', proceeding with deletion", len(poolVMs), pod)

	// 3. Stop all VMs and wait for them to be stopped
	log.Printf("Step 3: Stopping all running VMs in pool '%s'", pod)
	var runningVMs []proxmox.VM
	stoppedCount := 0

	for _, vm := range poolVMs {
		if vm.Type == "qemu" {
			// Only stop if VM is running
			if vm.RunningStatus == "running" {
				log.Printf("Force stopping VM '%s' (ID: %d) on node '%s'", vm.Name, vm.VmId, vm.NodeName)
				err := cm.ProxmoxService.StopVM(vm.NodeName, vm.VmId)
				if err != nil {
					log.Printf("ERROR: Failed to stop VM '%s' (ID: %d): %v", vm.Name, vm.VmId, err)
					return fmt.Errorf("failed to stop VM %s: %w", vm.Name, err)
				}

				// Only add to wait list if it was actually running
				runningVMs = append(runningVMs, proxmox.VM{
					Node: vm.NodeName,
					VMID: vm.VmId,
				})
				stoppedCount++
			} else {
				log.Printf("VM '%s' (ID: %d) is already stopped (status: %s)", vm.Name, vm.VmId, vm.RunningStatus)
			}
		} else {
			log.Printf("Skipping non-qemu resource '%s' (type: %s)", vm.Name, vm.Type)
		}
	}

	log.Printf("Initiated stop for %d running VMs, waiting for them to stop", stoppedCount)

	// Wait for all previously running VMs to be stopped
	for i, vm := range runningVMs {
		log.Printf("Waiting for VM %d/%d to stop: VMID %d on node %s", i+1, len(runningVMs), vm.VMID, vm.Node)
		err := cm.ProxmoxService.WaitForStopped(vm)
		if err != nil {
			log.Printf("WARNING: Timeout waiting for VM %d to stop: %v", vm.VMID, err)
			// Continue with deletion even if we can't confirm the VM is stopped
		} else {
			log.Printf("VM %d successfully stopped", vm.VMID)
		}
	}

	if len(runningVMs) > 0 {
		log.Printf("All %d VMs have been processed for stopping", len(runningVMs))
	}

	// 4. Delete all VMs
	log.Printf("Step 4: Deleting all VMs from pool '%s'", pod)
	deletedCount := 0

	for i, vm := range poolVMs {
		if vm.Type == "qemu" {
			log.Printf("Deleting VM %d/%d: '%s' (ID: %d) on node '%s'", i+1, len(poolVMs), vm.Name, vm.VmId, vm.NodeName)
			err := cm.ProxmoxService.DeleteVM(vm.NodeName, vm.VmId)
			if err != nil {
				log.Printf("ERROR: Failed to delete VM '%s' (ID: %d): %v", vm.Name, vm.VmId, err)
				return fmt.Errorf("failed to delete VM %s: %w", vm.Name, err)
			}
			deletedCount++
			log.Printf("Successfully initiated deletion of VM '%s' (ID: %d)", vm.Name, vm.VmId)
		}
	}

	log.Printf("Initiated deletion for %d VMs from pool '%s'", deletedCount, pod)

	// 5. Wait for all VMs to be deleted and pool to become empty
	log.Printf("Step 5: Waiting for all VMs to be deleted from pool '%s' (timeout: 5 minutes)", pod)
	err = cm.ProxmoxService.WaitForPoolEmpty(pod, 5*time.Minute)
	if err != nil {
		log.Printf("WARNING: %v", err)
		// Continue with pool deletion even if we can't confirm all VMs are gone
	} else {
		log.Printf("All VMs successfully deleted from pool '%s'", pod)
	}

	// 6. Delete the pool
	log.Printf("Step 6: Deleting pool '%s'", pod)
	err = cm.ProxmoxService.DeletePool(pod)
	if err != nil {
		log.Printf("ERROR: Failed to delete pool '%s': %v", pod, err)
		return fmt.Errorf("failed to delete pool %s: %w", pod, err)
	}

	log.Printf("Successfully deleted template pool '%s' and all its VMs", pod)
	log.Printf("Pod deletion process completed successfully for '%s'", pod)
	return nil
}
