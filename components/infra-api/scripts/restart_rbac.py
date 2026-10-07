#!/usr/bin/env python3
"""Render a review-only grant for one Deployment; never call kubectl or ACK."""
import argparse
import json
import re


def restart_rbac(namespace, deployment, role_id):
    label = r'[a-z0-9](?:[a-z0-9-]*[a-z0-9])?'
    if not re.fullmatch(label, namespace) or len(namespace) > 63:
        raise ValueError('a valid exact namespace is required')
    if len(deployment) > 253 or not all(re.fullmatch(label, part) and len(part) <= 63 for part in deployment.split('.')):
        raise ValueError('a valid exact Deployment name is required')
    if not re.fullmatch(r'[1-9][0-9]*', role_id):
        raise ValueError('verified numeric ACK RAM RoleId required')
    name = 'infra-api-restart'
    api = 'rbac.authorization.k8s.io/v1'
    return {'apiVersion':'v1', 'kind':'List', 'items':[
        {'apiVersion':api, 'kind':'Role', 'metadata':{'name':name,'namespace':namespace},
         'rules':[{'apiGroups':['apps'],'resources':['deployments'],'resourceNames':[deployment],'verbs':['get','patch']}]},
        {'apiVersion':api, 'kind':'RoleBinding', 'metadata':{'name':name,'namespace':namespace},
         'roleRef':{'apiGroup':'rbac.authorization.k8s.io','kind':'Role','name':name},
         'subjects':[{'kind':'User','apiGroup':'rbac.authorization.k8s.io','name':role_id}]}
    ]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--namespace', required=True)
    parser.add_argument('--deployment', required=True)
    parser.add_argument('--role-id', required=True, help='centrally verified numeric RoleId, not a guessed subject or role ARN')
    args = parser.parse_args()
    try:
        result = restart_rbac(args.namespace, args.deployment, args.role_id)
    except ValueError as error:
        parser.error(str(error))
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
