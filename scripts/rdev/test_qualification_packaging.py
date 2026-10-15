import pathlib,re,unittest
ROOT=pathlib.Path(__file__).resolve().parents[2]
class QualificationPackaging(unittest.TestCase):
 def test_guest_transfer_has_import_closure(self):
  go=(ROOT/'components/agent-gateway/internal/runtime/live.go').read_text()
  match=re.search(r'var harnessFiles = \[\]string\{([^}]+)\}',go)
  self.assertIsNotNone(match)
  transferred=set(re.findall(r'"([^"]+)"',match.group(1)))
  missing=[]
  for name in transferred:
   if not name.endswith('.mjs'):continue
   body=(ROOT/'components/agent-harness'/name).read_text()
   for target in re.findall(r"(?:from\s*|import\s*)['\"](\./[^'\"]+\.mjs)['\"]",body):
    if target[2:] not in transferred:missing.append((name,target))
  self.assertEqual(missing,[],f'AX guest transfer omits runtime imports: {missing}')

 def test_every_relative_gateway_harness_import_is_packaged(self):
  text=(ROOT/'components/agent-gateway/Dockerfile').read_text()
  line=next(x for x in text.splitlines() if x.startswith('COPY --from=harness '))
  copied={v.lstrip('/') for v in line.split()[2:-1]}
  missing=[]
  for name in copied:
   if not name.endswith('.mjs'):continue
   body=(ROOT/'components/agent-harness'/name).read_text()
   for target in re.findall(r"(?:from\s*|import\s*)['\"](\./[^'\"]+\.mjs)['\"]",body):
    if target[2:] not in copied:missing.append((name,target))
  self.assertEqual(missing,[],f'Runtime imports omitted from image: {missing}')
