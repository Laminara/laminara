package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/skin"
)

func (l *Loader) registerProviders(module string, specs []sdkmodule.ProviderSpec, svc sdkmodule.Service) {
	for _, spec := range specs {
		if err := registerProvider(spec, svc); err != nil {
			l.log.Error("модуль не отдал провайдер", "module", module, "provider", spec.Name, "error", err)
			continue
		}
		l.log.Info("модуль отдал провайдер", "module", module, "provider", spec.Name, "kind", kindName(spec.Kind))
	}
}

func registerProvider(spec sdkmodule.ProviderSpec, svc sdkmodule.Service) error {
	if spec.Name == "" {
		return errors.New("у провайдера нет имени")
	}
	switch spec.Kind {
	case sdkmodule.ProviderAuth:
		if err := free(spec.Name, auth.ProviderNames()); err != nil {
			return err
		}
		auth.RegisterProvider(spec.Name, func(config json.RawMessage) (auth.Provider, error) {
			handle, err := open(svc, spec, config)
			if err != nil {
				return nil, err
			}
			return &remoteAuth{svc: svc, handle: handle}, nil
		})
	case sdkmodule.ProviderSkin:
		if err := free(spec.Name, skin.ProviderNames()); err != nil {
			return err
		}
		skin.Register(spec.Name, func(config json.RawMessage) (skin.Provider, error) {
			handle, err := open(svc, spec, config)
			if err != nil {
				return nil, err
			}
			return &remoteSkin{svc: svc, handle: handle}, nil
		})
	case sdkmodule.ProviderAccess:
		if err := free(spec.Name, access.SourceNames()); err != nil {
			return err
		}
		access.RegisterSource(spec.Name, func(config json.RawMessage) (access.Source, error) {
			handle, err := open(svc, spec, config)
			if err != nil {
				return nil, err
			}
			return &remoteAccess{svc: svc, handle: handle}, nil
		})
	case sdkmodule.ProviderNews:
		if err := free(spec.Name, news.SourceNames()); err != nil {
			return err
		}
		news.RegisterSource(spec.Name, func(config json.RawMessage) (news.Source, error) {
			handle, err := open(svc, spec, config)
			if err != nil {
				return nil, err
			}
			return &remoteNews{svc: svc, handle: handle}, nil
		})
	default:
		return fmt.Errorf("неизвестный вид провайдера %d", spec.Kind)
	}
	return nil
}

func free(name string, taken []string) error {
	if slices.Contains(taken, name) {
		return fmt.Errorf("имя %q уже занято другим провайдером", name)
	}
	return nil
}

func open(svc sdkmodule.Service, spec sdkmodule.ProviderSpec, config json.RawMessage) (sdkmodule.Handle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), moduleLoadTimeout)
	defer cancel()
	return svc.OpenProvider(ctx, spec.Kind, spec.Name, config)
}

func kindName(kind sdkmodule.ProviderKind) string {
	switch kind {
	case sdkmodule.ProviderAuth:
		return "auth"
	case sdkmodule.ProviderSkin:
		return "skin"
	case sdkmodule.ProviderAccess:
		return "access"
	case sdkmodule.ProviderNews:
		return "news"
	default:
		return "unknown"
	}
}

type remoteAuth struct {
	svc    sdkmodule.Service
	handle sdkmodule.Handle
}

func (r *remoteAuth) Authenticate(ctx context.Context, creds auth.Credentials) (auth.Identity, error) {
	identity, err := r.svc.Authenticate(ctx, r.handle, sdkmodule.Credentials{
		Username:      creds.Username,
		Password:      creds.Password,
		TwoFactorCode: creds.TwoFactorCode,
	})
	switch {
	case errors.Is(err, sdkmodule.ErrTwoFactorRequired):
		return auth.Identity{}, auth.ErrTwoFactorRequired
	case errors.Is(err, sdkmodule.ErrInvalidCredentials):
		return auth.Identity{}, auth.ErrInvalidCredentials
	case err != nil:
		return auth.Identity{}, err
	}
	username := identity.Username
	if username == "" {
		username = creds.Username
	}
	subject := identity.Subject
	if subject == "" {
		subject = username
	}
	id, err := uuid.Parse(identity.UUID)
	if err != nil {
		id = auth.OfflineUUID(username)
	}
	return auth.Identity{Subject: subject, Username: username, UUID: id}, nil
}

type remoteSkin struct {
	svc    sdkmodule.Service
	handle sdkmodule.Handle
}

func (r *remoteSkin) Textures(ctx context.Context, username, uuid string) (skin.Textures, error) {
	textures, err := r.svc.Textures(ctx, r.handle, username, uuid)
	if err != nil {
		return skin.Textures{}, err
	}
	return skin.Textures{SkinURL: textures.SkinURL, CapeURL: textures.CapeURL, Slim: textures.Slim}, nil
}

type remoteAccess struct {
	svc    sdkmodule.Service
	handle sdkmodule.Handle
}

func (r *remoteAccess) Allows(ctx context.Context, build string, subject access.Subject) (bool, error) {
	return r.svc.Allows(ctx, r.handle, build, sdkmodule.Subject{
		Subject:  subject.Subject,
		Username: subject.Username,
		UUID:     subject.UUID,
	})
}

type remoteNews struct {
	svc    sdkmodule.Service
	handle sdkmodule.Handle
}

func (r *remoteNews) Items(ctx context.Context) ([]news.Item, error) {
	items, err := r.svc.NewsItems(ctx, r.handle)
	if err != nil {
		return nil, err
	}
	out := make([]news.Item, 0, len(items))
	for _, item := range items {
		out = append(out, news.Item{
			ID:          item.ID,
			Title:       item.Title,
			Body:        item.Body,
			PublishedAt: item.PublishedAt,
			Tag:         item.Tag,
			Link:        item.Link,
			Banner:      item.Banner,
		})
	}
	return out, nil
}
