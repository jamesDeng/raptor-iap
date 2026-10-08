import importlib.util
import pathlib
import unittest

class CertificateGateTests(unittest.TestCase):
    def evaluate(self, discovery, probe):
        source = pathlib.Path(__file__).with_name('preflight.py')
        self.assertTrue(source.exists(), 'certificate gate implementation missing')
        spec = importlib.util.spec_from_file_location('preflight', source)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.evaluate_certificate_gate(discovery, probe, {
            'podcertificaterequests': 'certificates.k8s.io/v1beta1',
            'clustertrustbundles': 'certificates.k8s.io/v1beta1'})

    def discovery(self):
        return {'certificates.k8s.io/v1beta1': ['podcertificaterequests', 'clustertrustbundles']}

    def test_missing_pcr_blocks(self):
        result = self.evaluate({'certificates.k8s.io/v1beta1': ['clustertrustbundles']}, {})
        self.assertFalse(result['ready'])
        self.assertIn('missing_resource:podcertificaterequests', result['reasons'])

    def test_wrong_version_blocks(self):
        result = self.evaluate({'certificates.k8s.io/v1': ['podcertificaterequests', 'clustertrustbundles']}, {})
        self.assertFalse(result['ready'])

    def test_discovery_without_projection_blocks(self):
        result = self.evaluate(self.discovery(), {})
        self.assertFalse(result['ready'])
        self.assertIn('projection_unverified', result['reasons'])

    def test_signing_denied_blocks(self):
        result = self.evaluate(self.discovery(), {'projection': True, 'approval': True, 'signing': False})
        self.assertFalse(result['ready'])
        self.assertIn('signing_unverified', result['reasons'])

    def test_string_true_does_not_pass(self):
        result = self.evaluate(self.discovery(), {'projection': 'true', 'approval': True, 'signing': True})
        self.assertFalse(result['ready'])

    def test_independent_api_versions_pass(self):
        source = pathlib.Path(__file__).with_name('preflight.py')
        spec = importlib.util.spec_from_file_location('preflight', source)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        result = module.evaluate_certificate_gate(
            {'certificates.k8s.io/v1': ['clustertrustbundles'],
             'certificates.k8s.io/v1beta1': ['podcertificaterequests']},
            {'projection': True, 'approval': True, 'signing': True},
            {r: ['certificates.k8s.io/v1', 'certificates.k8s.io/v1beta1']
             for r in ['podcertificaterequests', 'clustertrustbundles']})
        self.assertTrue(result['ready'])

    def test_supported_stable_version_passes(self):
        source = pathlib.Path(__file__).with_name('preflight.py')
        spec = importlib.util.spec_from_file_location('preflight', source)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        result = module.evaluate_certificate_gate(
            {'certificates.k8s.io/v1': ['podcertificaterequests', 'clustertrustbundles']},
            {'projection': True, 'approval': True, 'signing': True},
            {r: ['certificates.k8s.io/v1', 'certificates.k8s.io/v1beta1']
             for r in ['podcertificaterequests', 'clustertrustbundles']})
        self.assertTrue(result['ready'])

    def test_complete_probe_passes(self):
        result = self.evaluate(self.discovery(), {'projection': True, 'approval': True, 'signing': True})
        self.assertTrue(result['ready'])
        self.assertEqual(result['reasons'], [])

if __name__ == '__main__':
    unittest.main()
