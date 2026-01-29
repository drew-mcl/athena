package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/store"
)

type workItemCount struct {
	total     int
	completed int
}

func (d *Daemon) handleListWorkItems(params json.RawMessage) (any, error) {
	var req control.ListWorkItemsRequest
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}

	items, err := d.store.ListWorkItems(store.WorkItemFilter{
		Project:  req.Project,
		ItemType: req.ItemType,
		Status:   req.Status,
	})
	if err != nil {
		return nil, err
	}

	infos := make([]*control.WorkItemInfo, 0, len(items))
	for _, item := range items {
		infos = append(infos, workItemToInfo(item))
	}

	counts, _ := d.computeWorkItemCounts(req.Project)
	for _, info := range infos {
		if c, ok := counts[info.ID]; ok {
			info.TotalCount = c.total
			info.CompletedCount = c.completed
		}
	}

	return infos, nil
}

func (d *Daemon) handleGetWorkItem(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, fmt.Errorf("missing work item id")
	}

	item, err := d.store.GetWorkItem(req.ID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("work item not found: %s", req.ID)
	}

	info := workItemToInfo(item)
	counts, _ := d.computeWorkItemCounts(item.Project)
	if c, ok := counts[item.ID]; ok {
		info.TotalCount = c.total
		info.CompletedCount = c.completed
	}
	return info, nil
}

func (d *Daemon) handleCreateWorkItem(params json.RawMessage) (any, error) {
	var req control.CreateWorkItemRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	if req.Subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if req.ItemType == "" {
		return nil, fmt.Errorf("item_type is required")
	}

	project := req.Project
	var parent *store.WorkItem
	if req.ParentID != "" {
		var err error
		parent, err = d.store.GetWorkItem(req.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("parent work item not found: %s", req.ParentID)
		}
		if project == "" {
			project = parent.Project
		}
	}
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}

	status := req.Status
	if status == "" {
		status = string(store.WorkItemStatusPending)
	}

	priority := req.Priority
	if priority == 0 {
		priority = int(store.WorkItemPriorityNormal)
	}

	item := &store.WorkItem{
		Project:     project,
		ItemType:    store.WorkItemType(req.ItemType),
		Subject:     req.Subject,
		Description: req.Description,
		Status:      store.WorkItemStatus(status),
		Priority:    store.WorkItemPriority(priority),
	}
	if req.ParentID != "" {
		item.ParentID = &req.ParentID
	}
	if req.TicketID != "" {
		item.TicketID = &req.TicketID
	}

	created, err := d.store.CreateWorkItem(item)
	if err != nil {
		return nil, err
	}

	return workItemToInfo(created), nil
}

func (d *Daemon) handleUpdateWorkItem(params json.RawMessage) (any, error) {
	var req control.UpdateWorkItemRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	item, err := d.store.GetWorkItem(req.ID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("work item not found: %s", req.ID)
	}

	if req.Subject != "" {
		item.Subject = req.Subject
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.Status != "" {
		item.Status = store.WorkItemStatus(req.Status)
	}
	if req.Priority != nil {
		item.Priority = store.WorkItemPriority(*req.Priority)
	}
	if req.AgentID != "" {
		item.AgentID = &req.AgentID
	}

	if err := d.store.UpdateWorkItem(item); err != nil {
		return nil, err
	}

	return workItemToInfo(item), nil
}

func (d *Daemon) handleDeleteWorkItem(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	return map[string]bool{"success": true}, d.store.DeleteWorkItem(req.ID)
}

func (d *Daemon) handleGetWorkItemTree(params json.RawMessage) (any, error) {
	var req struct {
		RootID  string `json:"root_id"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	project := req.Project
	if req.RootID != "" {
		root, err := d.store.GetWorkItem(req.RootID)
		if err != nil {
			return nil, err
		}
		if root == nil {
			return []*control.WorkItemInfo{}, nil
		}
		project = root.Project
	}

	items, err := d.store.ListWorkItems(store.WorkItemFilter{Project: project})
	if err != nil {
		return nil, err
	}

	infos := make([]*control.WorkItemInfo, 0, len(items))
	byID := make(map[string]*control.WorkItemInfo, len(items))
	children := make(map[string][]*control.WorkItemInfo)
	for _, item := range items {
		info := workItemToInfo(item)
		infos = append(infos, info)
		byID[info.ID] = info
		if info.ParentID != "" {
			children[info.ParentID] = append(children[info.ParentID], info)
		}
	}

	if req.RootID != "" {
		if _, ok := byID[req.RootID]; !ok {
			return []*control.WorkItemInfo{}, nil
		}
		wanted := make(map[string]bool)
		stack := []string{req.RootID}
		wanted[req.RootID] = true
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, child := range children[id] {
				if wanted[child.ID] {
					continue
				}
				wanted[child.ID] = true
				stack = append(stack, child.ID)
			}
		}

		filtered := make([]*control.WorkItemInfo, 0, len(wanted))
		for _, info := range infos {
			if wanted[info.ID] {
				filtered = append(filtered, info)
			}
		}
		infos = filtered
	}

	counts := computeWorkItemCounts(infos)
	for _, info := range infos {
		if c, ok := counts[info.ID]; ok {
			info.TotalCount = c.total
			info.CompletedCount = c.completed
		}
	}

	return infos, nil
}

func (d *Daemon) handleGetWorkItemChildren(params json.RawMessage) (any, error) {
	var req struct {
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if req.ParentID == "" {
		return nil, fmt.Errorf("parent_id is required")
	}

	children, err := d.store.ListWorkItemChildren(req.ParentID)
	if err != nil {
		return nil, err
	}

	infos := make([]*control.WorkItemInfo, 0, len(children))
	for _, item := range children {
		infos = append(infos, workItemToInfo(item))
	}

	parent, _ := d.store.GetWorkItem(req.ParentID)
	project := ""
	if parent != nil {
		project = parent.Project
	}
	counts, _ := d.computeWorkItemCounts(project)
	for _, info := range infos {
		if c, ok := counts[info.ID]; ok {
			info.TotalCount = c.total
			info.CompletedCount = c.completed
		}
	}

	return infos, nil
}

func (d *Daemon) handleGetWorkItemAncestors(params json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	items, err := d.store.GetWorkItemAncestors(req.ID)
	if err != nil {
		return nil, err
	}

	infos := make([]*control.WorkItemInfo, 0, len(items))
	for _, item := range items {
		infos = append(infos, workItemToInfo(item))
	}

	return infos, nil
}

func (d *Daemon) handleGetReadyItems(params json.RawMessage) (any, error) {
	var req struct {
		Project string `json:"project"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
	}

	items, err := d.store.ListReadyWorkItems(req.Project)
	if err != nil {
		return nil, err
	}

	infos := make([]*control.WorkItemInfo, 0, len(items))
	for _, item := range items {
		infos = append(infos, workItemToInfo(item))
	}

	return infos, nil
}

func workItemToInfo(item *store.WorkItem) *control.WorkItemInfo {
	info := &control.WorkItemInfo{
		ID:          item.ID,
		Project:     item.Project,
		ItemType:    string(item.ItemType),
		Subject:     item.Subject,
		Description: item.Description,
		Status:      string(item.Status),
		Priority:    int(item.Priority),
		CreatedAt:   item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.Format(time.RFC3339),
	}
	if item.ParentID != nil {
		info.ParentID = *item.ParentID
	}
	if item.WorktreePath != nil {
		info.WorktreePath = *item.WorktreePath
	}
	if item.TicketID != nil {
		info.TicketID = *item.TicketID
	}
	if item.PRURL != nil {
		info.PRURL = *item.PRURL
	}
	if item.AgentID != nil {
		info.AgentID = *item.AgentID
	}
	return info
}

func (d *Daemon) computeWorkItemCounts(project string) (map[string]workItemCount, error) {
	items, err := d.store.ListWorkItems(store.WorkItemFilter{Project: project})
	if err != nil {
		return nil, err
	}
	infos := make([]*control.WorkItemInfo, 0, len(items))
	for _, item := range items {
		infos = append(infos, workItemToInfo(item))
	}
	return computeWorkItemCounts(infos), nil
}

func computeWorkItemCounts(items []*control.WorkItemInfo) map[string]workItemCount {
	counts := make(map[string]workItemCount, len(items))
	children := make(map[string][]*control.WorkItemInfo)
	for _, item := range items {
		if item.ParentID == "" {
			continue
		}
		children[item.ParentID] = append(children[item.ParentID], item)
	}

	var walk func(id string) (int, int)
	walk = func(id string) (int, int) {
		total := 0
		completed := 0
		for _, child := range children[id] {
			total++
			if child.Status == string(store.WorkItemStatusCompleted) {
				completed++
			}
			childTotal, childCompleted := walk(child.ID)
			total += childTotal
			completed += childCompleted
		}
		counts[id] = workItemCount{total: total, completed: completed}
		return total, completed
	}

	for _, item := range items {
		if _, ok := counts[item.ID]; ok {
			continue
		}
		walk(item.ID)
	}

	return counts
}
