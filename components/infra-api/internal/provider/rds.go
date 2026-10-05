package provider

import (
	"context"
	"encoding/json"
	"errors"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	rds "github.com/alibabacloud-go/rds-20140815/v11/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func cloudPages(config func(string) *openapi.Config) func(context.Context, domain.Environment, string, string, int) (page, error) {
	return func(ctx context.Context, env domain.Environment, kind, code string, n int) (page, error) {
		if ctx.Err() != nil {
			return page{}, ctx.Err()
		}
		if env.Region != "ap-southeast-1" {
			return page{}, domain.ErrScope
		}
		if kind == "db-proxy" {
			c, e := ess.NewClient(config("ess.ap-southeast-1.aliyuncs.com"))
			if e != nil {
				return page{}, e
			}
			r, e := c.DescribeScalingGroupsWithContext(ctx, &ess.DescribeScalingGroupsRequest{RegionId: tea.String(env.Region), PageNumber: tea.Int32(int32(n)), PageSize: tea.Int32(100), Tags: []*ess.DescribeScalingGroupsRequestTags{{Key: tea.String("env"), Value: tea.String(env.Code)}, {Key: tea.String("db-proxy-code"), Value: tea.String(code)}}}, nil)
			if e != nil {
				return page{}, e
			}
			if r == nil || r.Body == nil || r.Body.TotalCount == nil {
				return page{}, errors.New("missing provider page")
			}
			pg := page{Total: int(*r.Body.TotalCount)}
			for _, g := range r.Body.ScalingGroups {
				if g == nil {
					return page{}, errors.New("invalid provider record")
				}
				tags := map[string]string{}
				for _, t := range g.Tags {
					if t != nil {
						tags[tea.StringValue(t.TagKey)] = tea.StringValue(t.TagValue)
					}
				}
				pg.Records = append(pg.Records, record{ID: tea.StringValue(g.ScalingGroupId), Name: tea.StringValue(g.ScalingGroupName), State: tea.StringValue(g.LifecycleState), Tags: tags})
			}
			return pg, nil
		}
		c, e := rds.NewClient(config("rds.ap-southeast-1.aliyuncs.com"))
		if e != nil {
			return page{}, e
		}
		tagsJSON, _ := json.Marshal(map[string]string{"env": env.Code, "db-code": code})
		r, e := c.DescribeDBInstances(&rds.DescribeDBInstancesRequest{RegionId: tea.String(env.Region), PageNumber: tea.Int32(int32(n)), PageSize: tea.Int32(100), Tags: tea.String(string(tagsJSON))})
		if e != nil {
			return page{}, e
		}
		if r == nil || r.Body == nil || r.Body.TotalRecordCount == nil || r.Body.Items == nil {
			return page{}, errors.New("missing provider page")
		}
		pg := page{Total: int(*r.Body.TotalRecordCount)}
		for _, g := range r.Body.Items.DBInstance {
			if ctx.Err() != nil {
				return page{}, ctx.Err()
			}
			tr, e := c.DescribeTags(&rds.DescribeTagsRequest{RegionId: tea.String(env.Region), DBInstanceId: g.DBInstanceId})
			if e != nil {
				return page{}, e
			}
			if tr == nil || tr.Body == nil || tr.Body.Items == nil {
				return page{}, errors.New("missing tags")
			}
			tags := map[string]string{}
			for _, t := range tr.Body.Items.TagInfos {
				if t != nil {
					tags[tea.StringValue(t.TagKey)] = tea.StringValue(t.TagValue)
				}
			}
			pg.Records = append(pg.Records, record{ID: tea.StringValue(g.DBInstanceId), Name: tea.StringValue(g.DBInstanceDescription), State: tea.StringValue(g.DBInstanceStatus), Endpoint: tea.StringValue(g.ConnectionString), Tags: tags})
		}
		return pg, nil
	}
}
