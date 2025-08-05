package nfq

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"relloyd/tubetimeout/config"
	"relloyd/tubetimeout/group"
	"relloyd/tubetimeout/models"
	"relloyd/tubetimeout/monitor"
	"relloyd/tubetimeout/usage"
)

func TestNewNFQueueFilter(t *testing.T) {
	ctx := context.Background()
	logger := config.MustGetLogger()
	counter := monitor.NewTrafficMap(logger, 5)

	tracker, err := usage.NewTracker(ctx, logger, &config.AppCfg.TrackerConfig)
	assert.NoError(t, err, "unexpected error getting NewTrafficMap")

	manager := group.NewManager(logger)

	type args struct {
		cfg *config.FilterConfig
		t   models.TrackerI
		m   group.ManagerI
		c   monitor.TrafficCounter
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"nil tracker causes error", args{&config.AppCfg.FilterConfig, nil, manager, counter}, true},
		{"nil manager causes error", args{&config.AppCfg.FilterConfig, tracker, nil, counter}, true},
		{"nil counter causes error", args{&config.AppCfg.FilterConfig, tracker, manager, nil}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewNFQueueFilter(context.Background(), config.MustGetLogger(), tt.args.cfg, tt.args.t, tt.args.m, tt.args.c,
				func(*zap.Logger) {
					return
				},
			)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNFQueueFilter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
