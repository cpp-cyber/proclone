package cloning

import (
	"database/sql"
	"fmt"
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

	if config.RouterVMID == 0 || config.RouterNode == "" {
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
	var errors []string

	// 1. Get the template pool and its VMs
	templatePool, err := cm.ProxmoxService.GetPoolVMs(template)
	if err != nil {
		return fmt.Errorf("failed to get template pool: %w", err)
	}

	// 2. Check if the template has already been cloned by the user

	// Extract template name from pool name (remove kamino_template_ prefix)
	templateName := strings.TrimPrefix(template, "kamino_template_")

	targetPoolName := fmt.Sprintf("%s_%s", templateName, targetName)
	isDeployed, err := cm.IsDeployed(targetPoolName)
	if err != nil {
		return fmt.Errorf("failed to check if template is deployed: %w", err)
	}

	if isDeployed {
		return fmt.Errorf("template %s is already or in the process of being deployed %s", template, targetName)
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

	// 4. Verify that the pool is not empty
	if len(templateVMs) == 0 {
		return fmt.Errorf("template pool %s contains no VMs", template)
	}

	// 5. Get the next available pod ID and create new pool
	newPodID, newPodNumber, err := cm.ProxmoxService.GetNextPodID(cm.Config.MinPodID, cm.Config.MaxPodID)
	if err != nil {
		return fmt.Errorf("failed to get next pod ID: %w", err)
	}

	newPoolName := fmt.Sprintf("%s_%s_%s", newPodID, templateName, targetName)

	err = cm.ProxmoxService.CreateNewPool(newPoolName)
	if err != nil {
		return fmt.Errorf("failed to create new pool: %w", err)
	}

	// 6. Clone the router and all VMs

	// If no router was found in the template, use the default router template
	if router == nil {
		router = &proxmox.VM{
			Name: cm.Config.RouterName,
			Node: cm.Config.RouterNode,
			VMID: cm.Config.RouterVMID,
		}
	}

	newRouter, err := cm.ProxmoxService.CloneVM(*router, newPoolName)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to clone router VM: %v", err))
	}

	// Clone each VM to new pool
	for _, vm := range templateVMs {
		_, err := cm.ProxmoxService.CloneVM(vm, newPoolName)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to clone VM %s: %v", vm.Name, err))
		}
	}

	var vnetName = fmt.Sprintf("kamino%d", newPodNumber)

	// 7. Configure VNet of all VMs
	err = cm.SetPodVnet(newPoolName, vnetName)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to update pod vnet: %v", err))
	}

	// 8. Turn on Router
	if newRouter != nil {
		err = cm.ProxmoxService.WaitForDisk(newRouter.Node, newRouter.VMID, cm.Config.RouterWaitTimeout)
		if err != nil {
			errors = append(errors, fmt.Sprintf("router disk unavailable: %v", err))
		}

		err = cm.ProxmoxService.StartVM(newRouter.Node, newRouter.VMID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		}

		// 9. Wait for router to be running
		err = cm.ProxmoxService.WaitForRunning(*newRouter)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to start router VM: %v", err))
		} else {
			err = cm.configurePodRouter(newPodNumber, newRouter.Node, newRouter.VMID)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to configure pod router: %v", err))
			}
		}
	}

	// 10. Set permissions on the pool to the user/group
	err = cm.ProxmoxService.SetPoolPermission(newPoolName, targetName, isGroup)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to update pool permissions for %s: %v", targetName, err))
	}

	// 11. Add a +1 to the total deployments in the templates database
	err = cm.DatabaseService.AddDeployment(templateName)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to increment template deployments for %s: %v", templateName, err))
	}

	// If there were any errors, clean up if necessary
	if len(errors) > 0 {

		// Check if any VMs were successfully cloned
		clonedVMs, checkErr := cm.ProxmoxService.GetPoolVMs(newPoolName)
		if checkErr != nil {
		}

		if len(clonedVMs) == 0 {
			deleteErr := cm.ProxmoxService.DeletePool(newPoolName)
			if deleteErr != nil {
			}
		}

		return fmt.Errorf("clone operation completed with errors: %v", errors)
	}

	return nil
}

// DeletePod deletes a pod pool for a user or group
func (cm *CloningManager) DeletePod(pod string) error {

	// 1. Check if pool is already empty
	isEmpty, err := cm.ProxmoxService.IsPoolEmpty(pod)
	if err != nil {
		return fmt.Errorf("failed to check if pool %s is empty: %w", pod, err)
	}

	if isEmpty {
		err := cm.ProxmoxService.DeletePool(pod)
		if err != nil {
		} else {
		}
		return err
	}

	// 2. Get all virtual machines in the pool
	poolVMs, err := cm.ProxmoxService.GetPoolVMs(pod)
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
				err := cm.ProxmoxService.StopVM(vm.NodeName, vm.VmId)
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
	for _, vm := range runningVMs {
		err := cm.ProxmoxService.WaitForStopped(vm)
		if err != nil {
			// Continue with deletion even if we can't confirm the VM is stopped
		} else {
		}
	}

	if len(runningVMs) > 0 {
	}

	// 4. Delete all VMs
	deletedCount := 0

	for _, vm := range poolVMs {
		if vm.Type == "qemu" {
			err := cm.ProxmoxService.DeleteVM(vm.NodeName, vm.VmId)
			if err != nil {
				return fmt.Errorf("failed to delete VM %s: %w", vm.Name, err)
			}
			deletedCount++
		}
	}

	// 5. Wait for all VMs to be deleted and pool to become empty
	err = cm.ProxmoxService.WaitForPoolEmpty(pod, 5*time.Minute)
	if err != nil {
		// Continue with pool deletion even if we can't confirm all VMs are gone
	} else {
	}

	// 6. Delete the pool
	err = cm.ProxmoxService.DeletePool(pod)
	if err != nil {
		return fmt.Errorf("failed to delete pool %s: %w", pod, err)
	}

	return nil
}
