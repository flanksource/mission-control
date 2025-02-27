package auth

import (
	"errors"
	"os"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"gorm.io/gorm"
)

const (
	AdminName            = "Admin"
	AdminEmail           = "admin@local"
	DefaultAdminPassword = "admin"
)

func getDefaultAdminPassword() string {
	if password := os.Getenv("ADMIN_PASSWORD"); password != "" {
		return password
	}
	return DefaultAdminPassword
}

func GetOrCreateAdminUser(ctx context.Context) (*models.Person, error) {
	var admin models.Person
	if err := ctx.DB().Model(admin).Where("name = ? OR email = ?", AdminName, AdminEmail).First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			admin = models.Person{
				Name:  AdminName,
				Email: AdminEmail,
			}
			if err := ctx.DB().Create(&admin).Error; err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &admin, nil
}
