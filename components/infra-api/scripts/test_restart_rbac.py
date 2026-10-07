import unittest
from restart_rbac import restart_rbac

class RestartRBACTest(unittest.TestCase):
    def test_grant_is_one_namespaced_deployment_only(self):
        result = restart_rbac('infra-test', 'restart-target', '1234567890123456')
        role, binding = result['items']
        self.assertEqual(role['kind'], 'Role')
        self.assertEqual(role['metadata']['namespace'], 'infra-test')
        self.assertEqual(role['rules'], [{'apiGroups':['apps'], 'resources':['deployments'], 'resourceNames':['restart-target'], 'verbs':['get','patch']}])
        self.assertEqual(binding['kind'], 'RoleBinding')
        self.assertEqual(binding['metadata']['namespace'], 'infra-test')
        self.assertEqual(binding['subjects'], [{'kind':'User','apiGroup':'rbac.authorization.k8s.io','name':'1234567890123456'}])
        self.assertEqual(binding['roleRef']['name'], role['metadata']['name'])

    def test_invalid_identity_and_wildcard_targets_refuse(self):
        for namespace, deployment, role in [('*','app','123'), ('ns','*','123'), ('ns','app',''), ('ns','app','role/guessed'), ('Other Namespace','app','123')]:
            with self.subTest(namespace=namespace, deployment=deployment, role=role):
                with self.assertRaises(ValueError):
                    restart_rbac(namespace, deployment, role)

if __name__ == '__main__':
    unittest.main()
