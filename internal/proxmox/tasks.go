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

func (s *ProxmoxService) getTaskFromUPID(node string, upid string) (*Task, error) {
	taskReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/tasks/%s/status", node, upid),
	}

	var taskStatus Task
	if err := s.RequestHelper.MakeRequestAndUnmarshal(taskReq, &taskStatus); err != nil {
		return nil, err
	}
	return &taskStatus, nil
}

func (s *ProxmoxService) stopTask(node string, upid string) error {
	taskReq := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/nodes/%s/tasks/%s", node, upid),
	}

	_, err := s.RequestHelper.MakeRequest(taskReq)
	if err != nil {
		return err
	}

	return nil
}
