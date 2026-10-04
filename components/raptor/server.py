"""Bounded loopback API. This process never creates a cloud client."""
import argparse
from dataclasses import asdict
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import secrets
from urllib.parse import urlsplit, parse_qs
from tools.task_store.models import (Blocked, Conflict, NotFound, ValidationError,
                                     TaskBinding, exact, identifier)
from tools.task_store.store import Store
from tools.sandbox_lifecycle.paths import canonical_local_root
from tools.sandbox_lifecycle.state import protect_dir, safe_path

CSP="default-src 'self'; connect-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
ASSETS={'/':('index.html','text/html; charset=utf-8'), '/app.js':('app.js','text/javascript; charset=utf-8'),
        '/style.css':('style.css','text/css; charset=utf-8')}


class AccessDenied(Exception):
    pass


class TooLarge(Exception):
    pass


def load_token(root):
    path=safe_path(protect_dir(root)/'request-token')
    try:
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    except FileExistsError:
        os.chmod(path,0o600)
        value=path.read_text()
        if not re.fullmatch('[a-f0-9]{64}',value):
            raise ValueError('LocalTokenUnavailable')
        return value
    value=secrets.token_hex(32)
    with os.fdopen(fd,'w') as stream:
        stream.write(value);stream.flush();os.fsync(stream.fileno())
    return value


def unique_object(pairs):
    value={}
    for key,item in pairs:
        if key in value:
            raise ValidationError()
        value[key]=item
    return value


class LocalServer(ThreadingHTTPServer):
    daemon_threads=True
    def get_request(self):
        sock,address=super().get_request()
        sock.settimeout(10)
        return sock,address
    def handle_error(self,*_):
        # Neither traceback nor request body belongs in local diagnostics.
        pass


def make_server(store: Store, root: Path, port: int=8765) -> ThreadingHTTPServer:
    token=load_token(root)
    class Handler(BaseHTTPRequestHandler):
        protocol_version='HTTP/1.1'
        def log_message(self,*_):
            pass

        def _send(self,status,value=None,body=None,content_type='application/json; charset=utf-8'):
            data=json.dumps(value,ensure_ascii=False).encode('utf-8') if body is None else body
            self.send_response(status)
            for key,item in {'Content-Type':content_type,'Content-Length':str(len(data)),
                             'Cache-Control':'no-store','X-Content-Type-Options':'nosniff',
                             'Referrer-Policy':'no-referrer','Content-Security-Policy':CSP,
                             'Connection':'close'}.items():
                self.send_header(key,item)
            self.end_headers();self.close_connection=True
            self.wfile.write(data)

        def send_error(self,code,message=None,explain=None):
            self._send(code,{'error':'InvalidRequest'})

        def _access(self):
            host=f'127.0.0.1:{self.server.server_address[1]}'
            origin='http://'+host
            for header in ('Host','Origin','X-Raptor-Token','Sec-Fetch-Site'):
                if len(self.headers.get_all(header,[]))>1:
                    raise AccessDenied()
            if self.headers.get('Host')!=host:
                raise AccessDenied()
            if ('Origin' in self.headers and self.headers['Origin']!=origin) or self.headers.get('Sec-Fetch-Site')=='cross-site':
                raise AccessDenied()
            if self.command in ('POST','PUT'):
                supplied=self.headers.get('X-Raptor-Token','')
                if self.headers.get('Origin')!=origin or not supplied.isascii() or not hmac.compare_digest(supplied,token):
                    raise AccessDenied()

        def _body(self):
            lengths=self.headers.get_all('Content-Length',[])
            if ('Transfer-Encoding' in self.headers or len(lengths)!=1
                    or not lengths[0].isascii() or not lengths[0].isdigit()
                    or self.headers.get('Content-Type')!='application/json'):
                raise ValidationError()
            size=int(lengths[0])
            if size>65536:raise TooLarge()
            data=self.rfile.read(size)
            if len(data)!=size:raise ValidationError()
            try:
                value=json.loads(data.decode('utf-8'),object_pairs_hook=unique_object,
                                 parse_constant=lambda _:(_ for _ in ()).throw(ValidationError()))
            except (UnicodeError,json.JSONDecodeError):
                raise ValidationError() from None
            if type(value) is not dict:raise ValidationError()
            return value

        def _route(self):
            self._access()
            url=urlsplit(self.path)
            if url.scheme or url.netloc or url.fragment:raise ValidationError()
            path=url.path
            query=parse_qs(url.query,keep_blank_values=True)
            if self.command=='GET' and path in ASSETS and not query:
                name,mime=ASSETS[path];file=Path(__file__).parent/'web'/name
                if not file.is_file():raise NotFound()
                self._send(200,body=file.read_bytes(),content_type=mime);return
            if path=='/api/session' and self.command=='GET' and not query:
                self._send(200,{'data':{'token':token}});return
            if query and (path!='/api/tasks' or self.command!='GET' or set(query)!={'app_id'} or len(query['app_id'])!=1):
                raise ValidationError()
            if query:
                identifier(query['app_id'][0])
            value=self._body() if self.command in ('POST','PUT') else None
            status=200
            if path=='/api/apps':
                if self.command=='GET':data=store.list_apps()
                elif self.command=='POST':data=store.create_app(value);status=201
                else:raise NotFound()
            elif path=='/api/tasks':
                if self.command=='GET':data=store.list_tasks(query.get('app_id',[None])[0])
                elif self.command=='POST':data=store.submit(value);status=201
                else:raise NotFound()
            else:
                parts=path.strip('/').split('/')
                if len(parts)<3 or parts[0]!='api' or parts[1] not in ('apps','tasks'):raise NotFound()
                item=identifier(parts[2])
                if len(parts)==3:
                    if parts[1]=='apps' and self.command=='GET':data=store.get_app(item)
                    elif parts[1]=='apps' and self.command=='PUT':
                        exact(value,('expected_version','app'))
                        data=store.update_app(item,value['expected_version'],value['app'])
                    elif parts[1]=='tasks' and self.command=='GET':data=store.get_task(item)
                    else:raise NotFound()
                elif len(parts)==4 and parts[1]=='tasks' and self.command=='POST':
                    task=store.get_task(item)
                    if parts[3]=='cancel':
                        exact(value,('attempt_id',));store.cancel(item,value['attempt_id']);data=store.get_task(item)
                    elif parts[3]=='retry':
                        exact(value,('idempotency_key',));data=store.retry(item,value['idempotency_key'])
                    elif parts[3]=='recover':
                        exact(value,('attempt_id','idempotency_key'))
                        binding=TaskBinding(item,value['attempt_id'],task['snapshot_sha256'])
                        store.request_recovery(binding,value['idempotency_key']);data={'requested':True};status=202
                    else:raise NotFound()
                else:raise NotFound()
            self._send(status,{'data':data})

        def _handle(self):
            try:self._route()
            except AccessDenied:self._send(403,{'error':'LocalAccessDenied'})
            except TooLarge:self._send(413,{'error':'RequestTooLarge'})
            except ValidationError:self._send(400,{'error':'InvalidInput'})
            except NotFound:self._send(404,{'error':'NotFound'})
            except (Conflict,Blocked):self._send(409,{'error':'ConflictOrBlocked'})
            except Exception:self._send(500,{'error':'LocalPersistenceFailed'})

        do_GET=do_POST=do_PUT=_handle

    return LocalServer(('127.0.0.1',port),Handler)


def main(argv=None):
    parser=argparse.ArgumentParser(description='Serve the local single-owner Raptor UI on 127.0.0.1:8765.')
    parser.parse_args(argv)
    root=canonical_local_root()/'raptor'
    server=make_server(Store(root),root)
    print('Raptor ready at http://127.0.0.1:8765')
    try:server.serve_forever()
    except KeyboardInterrupt:pass
    finally:server.server_close()


if __name__=='__main__':main()
