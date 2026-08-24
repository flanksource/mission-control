package main

import (
	"context"
	"sort"

	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
)

type accessGroupData struct {
	groups  []sdk.ExternalGroupSummary
	members []sdk.GroupMember
	byUser  map[uuid.UUID][]RegisterGroup
}

func loadAccessGroups(ctx context.Context, client *sdk.Client, options accessExportOptions) (accessGroupData, error) {
	groups, total, err := client.ListExternalGroups(ctx, sdk.IdentityOptions{Limit: options.Limit})
	if err != nil {
		return accessGroupData{}, err
	}
	if options.RequireComplete {
		if err := requireCompleteProjection("groups", len(groups), total, options.Limit); err != nil {
			return accessGroupData{}, err
		}
	}
	warnTruncated("groups", len(groups), total)

	ids := make([]string, 0, len(groups))
	groupsByID := make(map[uuid.UUID]sdk.ExternalGroupSummary, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID.String())
		groupsByID[group.ID] = group
	}
	members, err := client.GetGroupMembers(ctx, ids)
	if err != nil {
		return accessGroupData{}, err
	}

	byUser := map[uuid.UUID][]RegisterGroup{}
	for _, member := range members {
		if !member.Active() {
			continue
		}
		group, ok := groupsByID[member.GroupID]
		if !ok {
			continue
		}
		byUser[member.UserID] = append(byUser[member.UserID], RegisterGroup{
			ID: group.ID.String(), Name: group.Name, GroupType: group.GroupType, Tenant: group.Tenant,
		})
	}
	for userID := range byUser {
		sort.Slice(byUser[userID], func(i, j int) bool { return byUser[userID][i].ID < byUser[userID][j].ID })
	}
	return accessGroupData{groups: groups, members: members, byUser: byUser}, nil
}

func projectRegisterGroups(groups []sdk.ExternalGroupSummary, grants []sdk.AccessGrant, members []sdk.GroupMember) []RegisterIdentity {
	grantsByGroup := map[uuid.UUID][]sdk.AccessGrant{}
	for _, grant := range grants {
		if grant.ExternalGroupID != nil {
			grantsByGroup[*grant.ExternalGroupID] = append(grantsByGroup[*grant.ExternalGroupID], grant)
		}
	}
	membersByGroup := map[uuid.UUID][]RegisterMember{}
	for _, member := range members {
		if !member.Active() {
			continue
		}
		membersByGroup[member.GroupID] = append(membersByGroup[member.GroupID], RegisterMember{
			ExternalUserID: member.UserID.String(), Name: member.UserName, Email: member.Email,
			UserType: member.UserType, AddedAt: registerDate(&member.MembershipAddedAt),
			LastSignedInAt: registerDate(member.LastSignedInAt),
		})
	}

	entries := make([]RegisterIdentity, 0, len(groups))
	for _, group := range groups {
		held := grantsByGroup[group.ID]
		if len(held) == 0 {
			continue
		}
		groupMembers := membersByGroup[group.ID]
		if groupMembers == nil {
			groupMembers = []RegisterMember{}
		}
		sort.Slice(groupMembers, func(i, j int) bool { return groupMembers[i].ExternalUserID < groupMembers[j].ExternalUserID })
		aliases := []string(group.Aliases)
		if aliases == nil {
			aliases = []string{}
		}
		entries = append(entries, RegisterIdentity{
			ID: "external-group-" + group.ID.String(), IdentityType: "group",
			IdentityProvider: identityProvider(held), Name: group.Name,
			Aliases: aliases, ExternalGroupID: group.ID.String(),
			GroupType: group.GroupType, Tenant: group.Tenant, ProvisionedAt: registerDate(&group.CreatedAt),
			Members: groupMembers, ConfigAccess: registerGrants(held),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}
