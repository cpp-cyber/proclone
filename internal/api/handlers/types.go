package handlers

import "github.com/cpp-cyber/proclone/internal/cloning"

// API endpoint request structures

type VMActionRequest struct {
	Node string `json:"node"`
	VMID int    `json:"vmid"`
}

type TemplateRequest struct {
	Template string `json:"template"`
}

type PublishTemplateRequest struct {
	Template cloning.KaminoTemplate `json:"template"`
}

type CloneRequest struct {
	Template string `json:"template"`
}

type GroupsRequest struct {
	Groups []string `json:"groups"`
}

type AdminCloneRequest struct {
	Template  string   `json:"template"`
	Usernames []string `json:"usernames"`
	Groups    []string `json:"groups"`
}

type DeletePodRequest struct {
	Pod string `json:"pod"`
}

type AdminDeletePodRequest struct {
	Pods []string `json:"pods"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminCreateUserRequest struct {
	Users []CreateUserRequest `json:"users"`
}

type UsersRequest struct {
	Usernames []string `json:"usernames"`
}

type ModifyGroupMembersRequest struct {
	Group     string   `json:"group"`
	Usernames []string `json:"usernames"`
}

type SetUserGroupsRequest struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

type RenameGroupRequest struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}
