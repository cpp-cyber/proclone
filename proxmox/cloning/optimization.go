package cloning

import (
	"fmt"
	"math"

	"github.com/cpp-cyber/proclone/proxmox"
)

/*
 * ----- FIND OPTIMAL COMPUTE NODE FOR NEXT VM CLONE -----
 * this function factors in current memory & cpu utilization,
 * total memory allocation, and vm density to decide which node
 * has the best resource availability for new VMs
 */
func findBestNode(config *proxmox.ProxmoxConfig) (node string, err error) {

	// define variables and data structures to hold structured values relevant to calculating optimal node
	var totalVms int
	vmDensityMap := make(map[string]int)
	allocatedMemoryMap := make(map[string]int64)
	totalMemoryMap := make(map[string]int64)
	currentMemoryMap := make(map[string]int64)
	cpuUtilizationMap := make(map[string]float64)

	virtualMachines, err := proxmox.GetVirtualMachineResponse(config)

	if err != nil {
		return "", fmt.Errorf("failed to get cluster resources: %v", err)
	}

	// increment density and allocated memory values per vm
	for _, machine := range *virtualMachines {
		if machine.Template != 1 {
			allocatedMemoryMap[machine.NodeName] += int64(machine.MaxMem)
			vmDensityMap[machine.NodeName] += 1
			totalVms += 1
		}
	}

	// set default topScore to lowest possible float32 value
	var topScore float32 = -1 * math.MaxFloat32
	var bestNode string

	for _, node := range config.Nodes {
		nodeStatus, err := proxmox.GetNodeStatus(config, node)
		if err != nil {
			return "", fmt.Errorf("failed to get node status of %s: %v", node, err)
		}

		// set total and current memory values, and cpu utilization values for eaach node
		totalMemoryMap[node] = nodeStatus.Memory.Total
		currentMemoryMap[node] = nodeStatus.Memory.Used
		cpuUtilizationMap[node] = nodeStatus.CPU

		// fraction of node memory that is currently free
		freeMemRatio := 1 - float32(currentMemoryMap[node])/float32(totalMemoryMap[node])

		// fraction of free node cpu resources
		freeCpuRatio := 1 - float32(cpuUtilizationMap[node])

		// fraction of node memory that is currently unallocated
		unallocatedMemRatio := 1 - float32(allocatedMemoryMap[node])/float32(totalMemoryMap[node])

		// inverse vm density value (higher is better)
		inverseVmDensity := 1 - float32(vmDensityMap[node])/float32(totalVms)

		// calculate node score (higher is better)
		score :=
			0.40*freeMemRatio +
				0.25*freeCpuRatio +
				0.30*unallocatedMemRatio +
				0.05*inverseVmDensity

		// if node score is higher than current bestNode, update bestNode
		if score > topScore {
			topScore = score
			bestNode = node
		}
	}
	return bestNode, nil
}
