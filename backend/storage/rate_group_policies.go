package storage

import (
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RateGroupPolicies struct{ db *gorm.DB }

func NewRateGroupPolicies(db *gorm.DB) *RateGroupPolicies {
	return &RateGroupPolicies{db: db}
}

func RateGroupKey(remoteGroupID *int64, groupName string) string {
	if remoteGroupID != nil {
		return "id:" + strconv.FormatInt(*remoteGroupID, 10)
	}
	return "name:" + strings.ToLower(strings.TrimSpace(groupName))
}

func (r *RateGroupPolicies) ListByChannel(channelID uint) ([]RateGroupPolicy, error) {
	var list []RateGroupPolicy
	if err := r.db.Where("channel_id = ?", channelID).Order("group_name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *RateGroupPolicies) List() ([]RateGroupPolicy, error) {
	var list []RateGroupPolicy
	if err := r.db.Order("channel_id ASC").Order("group_name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *RateGroupPolicies) Find(channelID uint, groupKey string) (*RateGroupPolicy, error) {
	var item RateGroupPolicy
	err := r.db.Where("channel_id = ? AND group_key = ?", channelID, groupKey).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *RateGroupPolicies) Upsert(item *RateGroupPolicy) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "group_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"remote_group_id", "group_name", "max_ratio", "calculation_ratio", "updated_at",
		}),
	}).Create(item).Error
}

func (r *RateGroupPolicies) Delete(channelID uint, groupKey string) error {
	return r.db.Where("channel_id = ? AND group_key = ?", channelID, groupKey).Delete(&RateGroupPolicy{}).Error
}
