package gcp

import (
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/storage/v1"
)

func findInstanceInAggregatedPages(instanceName string, fetch func(pageToken string) (*compute.InstanceAggregatedList, error)) (*compute.Instance, error) {
	pageToken := ""

	for {
		list, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		for _, scopedList := range list.Items {
			for _, instance := range scopedList.Instances {
				if instance.Name == instanceName {
					return instance, nil
				}
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return nil, ErrInstanceNotFound
}

func findMIGScopeAcrossPages(migName string, fetch func(pageToken string) (*compute.InstanceGroupManagerAggregatedList, error)) (string, error) {
	pageToken := ""

	for {
		list, err := fetch(pageToken)
		if err != nil {
			return "", err
		}

		for scopeName, scopedList := range list.Items {
			for _, mig := range scopedList.InstanceGroupManagers {
				if mig.Name == migName {
					return scopeName, nil
				}
			}
		}

		if list.NextPageToken == "" {
			break
		}

		pageToken = list.NextPageToken
	}

	return "", ErrMIGNotInAggregatedList
}

func collectInstances(fetch func(pageToken string) ([]*compute.Instance, string, error)) ([]*compute.Instance, error) {
	pageToken := ""
	var all []*compute.Instance

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectInstanceGroupManagers(fetch func(pageToken string) ([]*compute.InstanceGroupManager, string, error)) ([]*compute.InstanceGroupManager, error) {
	pageToken := ""
	var all []*compute.InstanceGroupManager

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectManagedInstances(fetch func(pageToken string) ([]*compute.ManagedInstance, string, error)) ([]*compute.ManagedInstance, error) {
	pageToken := ""
	var all []*compute.ManagedInstance

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectZones(fetch func(pageToken string) ([]*compute.Zone, string, error)) ([]*compute.Zone, error) {
	pageToken := ""
	var all []*compute.Zone

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectInstanceTemplates(fetch func(pageToken string) ([]*compute.InstanceTemplate, string, error)) ([]*compute.InstanceTemplate, error) {
	pageToken := ""
	var all []*compute.InstanceTemplate

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectSnapshots(fetch func(pageToken string) ([]*compute.Snapshot, string, error)) ([]*compute.Snapshot, error) {
	pageToken := ""
	var all []*compute.Snapshot

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectBuckets(fetch func(pageToken string) ([]*storage.Bucket, string, error)) ([]*storage.Bucket, error) {
	pageToken := ""
	var all []*storage.Bucket

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectHealthChecks(fetch func(pageToken string) ([]*compute.HealthCheck, string, error)) ([]*compute.HealthCheck, error) {
	pageToken := ""
	var all []*compute.HealthCheck

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}

func collectURLMaps(fetch func(pageToken string) ([]*compute.UrlMap, string, error)) ([]*compute.UrlMap, error) {
	pageToken := ""
	var all []*compute.UrlMap

	for {
		items, nextToken, err := fetch(pageToken)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)

		if nextToken == "" {
			break
		}

		pageToken = nextToken
	}

	return all, nil
}
