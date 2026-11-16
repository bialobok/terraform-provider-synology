package core

import (
	"context"
	"sort"

	"github.com/synology-community/go-synology/pkg/api"
	"github.com/synology-community/go-synology/pkg/api/core"
	"github.com/synology-community/go-synology/pkg/api/core/methods"
)

// Calls the generated client doesn't cover: the user list with its extra
// fields, and group membership (SYNO.Core.Group.Member), which is how users
// are added to and removed from groups in place.

// userDetail is a user as SYNO.Core.User list returns it with additional
// fields. The booleans are pointers: DSM may leave out fields it doesn't
// support, and then Read keeps what's in state.
type userDetail struct {
	Name              string `json:"name"`
	UID               int    `json:"uid"`
	Description       string `json:"description"`
	Email             string `json:"email"`
	Expired           string `json:"expired"`
	PasswdNeverExpire *bool  `json:"passwd_never_expire"`
	CannotChgPasswd   *bool  `json:"cannot_chg_passwd"`
}

type userDetailList struct {
	Users []userDetail `json:"users"`
	Total int          `json:"total"`
}

type userListDetailRequest struct {
	Type       string   `url:"type"`
	Offset     int      `url:"offset"`
	Limit      int      `url:"limit"`
	Additional []string `url:"additional,json"`
}

var userListAdditional = []string{
	"description", "email", "expired", "passwd_never_expire", "cannot_chg_passwd",
}

func listUserDetails(ctx context.Context, raw api.Api) ([]userDetail, error) {
	res, err := api.Post[userDetailList](raw, ctx, &userListDetailRequest{
		Type:       "local",
		Offset:     0,
		Limit:      -1,
		Additional: userListAdditional,
	}, methods.UserList)
	if err != nil {
		return nil, err
	}
	return res.Users, nil
}

func findUserDetail(users []userDetail, name string) *userDetail {
	for i := range users {
		if users[i].Name == name {
			return &users[i]
		}
	}
	return nil
}

var (
	groupMemberList = api.Method{
		API:            "SYNO.Core.Group.Member",
		Version:        1,
		Method:         api.MethodList,
		ErrorSummaries: api.GlobalErrors,
	}
	groupMemberChange = api.Method{
		API:            "SYNO.Core.Group.Member",
		Version:        1,
		Method:         "change",
		ErrorSummaries: api.GlobalErrors,
	}
)

type groupMemberListRequest struct {
	Group   string `url:"group"`
	InGroup bool   `url:"ingroup"`
	Offset  int    `url:"offset"`
	Limit   int    `url:"limit"`
}

type groupMemberListResponse struct {
	Users []struct {
		Name string `json:"name"`
	} `json:"users"`
}

type groupMemberChangeRequest struct {
	Group  string   `url:"group"`
	Add    []string `url:"add_member,json,omitempty"`
	Remove []string `url:"remove_member,json,omitempty"`
}

// userGroups returns the sorted names of the groups the user belongs to.
func userGroups(ctx context.Context, raw api.Api, c core.Api, user string) ([]string, error) {
	groups, err := c.GroupList(ctx)
	if err != nil {
		return nil, err
	}
	var in []string
	for _, g := range groups.Groups {
		members, err := api.Post[groupMemberListResponse](raw, ctx, &groupMemberListRequest{
			Group: g.Name, InGroup: true, Offset: 0, Limit: -1,
		}, groupMemberList)
		if err != nil {
			return nil, err
		}
		for _, m := range members.Users {
			if m.Name == user {
				in = append(in, g.Name)
				break
			}
		}
	}
	sort.Strings(in)
	return in, nil
}

// groupDiff returns which groups to join and which to leave.
func groupDiff(have, want []string) (join, leave []string) {
	h := map[string]bool{}
	for _, g := range have {
		h[g] = true
	}
	w := map[string]bool{}
	for _, g := range want {
		w[g] = true
		if !h[g] {
			join = append(join, g)
		}
	}
	for _, g := range have {
		if !w[g] {
			leave = append(leave, g)
		}
	}
	sort.Strings(join)
	sort.Strings(leave)
	return join, leave
}

// setUserGroups moves the user into exactly the wanted groups.
func setUserGroups(ctx context.Context, raw api.Api, user string, have, want []string) error {
	join, leave := groupDiff(have, want)
	for _, g := range join {
		if err := api.PostVoid(raw, ctx, &groupMemberChangeRequest{
			Group: g, Add: []string{user},
		}, groupMemberChange); err != nil {
			return err
		}
	}
	for _, g := range leave {
		if err := api.PostVoid(raw, ctx, &groupMemberChangeRequest{
			Group: g, Remove: []string{user},
		}, groupMemberChange); err != nil {
			return err
		}
	}
	return nil
}
