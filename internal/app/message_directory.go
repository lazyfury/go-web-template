package app

import (
	"context"

	"gorm.io/gorm"

	"github.com/example/go-web-template/internal/message"
	"github.com/example/go-web-template/internal/platform/apperr"
	"github.com/example/go-web-template/internal/user"
)

// messageDirectory adapts the user table to message.Directory. Keeping the
// query in the composition root means the message domain never imports user.
type messageDirectory struct {
	db *gorm.DB
}

func (d messageDirectory) Resolve(ctx context.Context, filter message.RecipientFilter) ([]message.Contact, error) {
	query := d.db.WithContext(ctx).
		Model(&user.User{}).
		Where("status = ?", user.StatusActive)

	switch {
	case filter.All:
		// No additional filter.
	case len(filter.UserIDs) > 0:
		query = query.Where("id IN ?", filter.UserIDs)
	case len(filter.Roles) > 0:
		roles := make([]string, 0, len(filter.Roles))
		for _, role := range filter.Roles {
			roles = append(roles, string(role))
		}
		query = query.Where("role IN ?", roles)
	default:
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, "empty recipient filter", nil)
	}

	var rows []struct {
		ID    uint64
		Email string
		Phone string
	}
	if err := query.Select("id, email, phone").Order("id ASC").Scan(&rows).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabase, "resolve message recipients failed", err)
	}
	contacts := make([]message.Contact, 0, len(rows))
	for _, row := range rows {
		contacts = append(contacts, message.Contact{
			UserID: row.ID,
			Email:  row.Email,
			Phone:  row.Phone,
		})
	}
	return contacts, nil
}

var _ message.Directory = messageDirectory{}
