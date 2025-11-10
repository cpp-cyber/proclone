package proxmox

import (
	"fmt"

	"github.com/cpp-cyber/proclone/internal/tools"
)

func (s *ProxmoxService) getActiveCloningTasks(node string) ([]Task, error) {
	activeCloningReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/tasks?source=active&typefilter=qmclone", node),
	}

	var activeCloningTasks []Task
	if err := s.RequestHelper.MakeRequestAndUnmarshal(activeCloningReq, &activeCloningTasks); err != nil {
		return nil, err
	}
	return activeCloningTasks, nil
}
