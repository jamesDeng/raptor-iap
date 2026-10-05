import unittest
from preflight import validate

class PreflightTests(unittest.TestCase):
    def valid(self):
        return dict(region="ap-southeast-1", worker_cores=4, worker_memory_gb=8, worker_count=1, worker_available=True, hours=72, budget_cny=150, estimated_total_cny=130, prices_complete=True, source_integrated=True, credentials_valid=True)
    def test_valid_foundation(self):
        self.assertEqual(validate(self.valid()), [])
    def test_fail_closed_on_cost_or_unknown_prices(self):
        for changes in [dict(estimated_total_cny=151), dict(prices_complete=False)]:
            self.assertTrue(validate(self.valid() | changes))
    def test_reject_undersized_worker(self):
        self.assertTrue(validate(self.valid() | dict(worker_cores=2)))
    def test_release_requires_integrated_source_and_stock(self):
        for key in ["source_integrated", "worker_available", "credentials_valid"]:
            self.assertTrue(validate(self.valid() | {key:False}))
    def test_missing_fields_fail_closed(self):
        self.assertTrue(validate({}))

if __name__ == "__main__": unittest.main()
