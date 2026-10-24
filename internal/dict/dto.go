package dict

import "time"

// CreateGroupRequest is the payload for POST /dict-groups.
type CreateGroupRequest struct {
	Key         string  `json:"key" binding:"required,min=2,max=64"`
	Name        string  `json:"name" binding:"required,min=1,max=64"`
	Description string  `json:"description" binding:"max=255"`
	Status      *Status `json:"status" binding:"omitempty,oneof=0 1"`
}

// UpdateGroupRequest is the payload for PATCH /dict-groups/:id. The key is not
// part of the patch: consumers reference it from code.
type UpdateGroupRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=64"`
	Description *string `json:"description" binding:"omitempty,max=255"`
	Status      *Status `json:"status" binding:"omitempty,oneof=0 1"`
}

// CreateItemRequest is the payload for POST /dict-groups/:id/items.
type CreateItemRequest struct {
	Key       string    `json:"key" binding:"required,max=64"`
	Label     string    `json:"label" binding:"max=128"`
	Value     string    `json:"value" binding:"max=1024"`
	ValueType ValueType `json:"value_type" binding:"omitempty,oneof=string number boolean json"`
	SortOrder int       `json:"sort"`
	Status    *Status   `json:"status" binding:"omitempty,oneof=0 1"`
	Remark    string    `json:"remark" binding:"max=255"`
}

// UpdateItemRequest is the payload for PATCH /dict-items/:id.
type UpdateItemRequest struct {
	Key       *string    `json:"key" binding:"omitempty,min=1,max=64"`
	Label     *string    `json:"label" binding:"omitempty,min=1,max=128"`
	Value     *string    `json:"value" binding:"omitempty,max=1024"`
	ValueType *ValueType `json:"value_type" binding:"omitempty,oneof=string number boolean json"`
	SortOrder *int       `json:"sort"`
	Status    *Status    `json:"status" binding:"omitempty,oneof=0 1"`
	Remark    *string    `json:"remark" binding:"omitempty,max=255"`
}

// GroupDTO is the HTTP representation of a group.
type GroupDTO struct {
	ID          uint64    `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      Status    `json:"status" enum:"0,1"`
	ItemCount   int64     `json:"item_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GroupListResponse is the payload for GET /dict-groups.
type GroupListResponse struct {
	Items    []GroupDTO `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ItemDTO is the HTTP representation of one element of a dict array.
type ItemDTO struct {
	ID        uint64    `json:"id"`
	GroupID   uint64    `json:"group_id"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	Value     string    `json:"value"`
	ValueType ValueType `json:"value_type" enum:"string,number,boolean,json"`
	Sort      int       `json:"sort"`
	Status    Status    `json:"status" enum:"0,1"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toGroupDTO(group *Group, itemCount int64) GroupDTO {
	return GroupDTO{
		ID:          group.ID,
		Key:         group.Key,
		Name:        group.Name,
		Description: group.Description,
		Status:      group.Status,
		ItemCount:   itemCount,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
}

func toItemDTO(item *Item) ItemDTO {
	return ItemDTO{
		ID:        item.ID,
		GroupID:   item.GroupID,
		Key:       item.Key,
		Label:     item.Label,
		Value:     item.EffectiveValue(),
		ValueType: item.ValueType,
		Sort:      item.SortOrder,
		Status:    item.Status,
		Remark:    item.Remark,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func toItemDTOs(items []Item) []ItemDTO {
	out := make([]ItemDTO, 0, len(items))
	for i := range items {
		out = append(out, toItemDTO(&items[i]))
	}
	return out
}
