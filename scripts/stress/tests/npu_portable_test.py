#!/usr/bin/env python3
"""CPU-only regressions against the *applied* public compatibility patch."""
import importlib
import ast
import csv
import json
import fcntl
import logging
import multiprocessing as mp
import os
from pathlib import Path
import queue
import subprocess
import sys
import tempfile
import threading
import time
import types
import unittest

# Called with a freshly copied + patched tree by the shell test.
if __name__ == '__main__':
    sys.path.insert(0, sys.argv.pop(1))
from ascend_npu_burn import runtime_compat as compat


def fail_worker():
    import catmonitor_missing_worker_import_fixture


def sleep_worker():
    time.sleep(30)


def csv_worker(output, device):
    # Execute the actual patched writer without importing hardware modules.
    source = (Path(compat.__file__).parent/'runtime/engine.py').read_text()
    function = next(n for n in ast.parse(source).body if isinstance(n,ast.FunctionDef) and n.name=='generate_csv_report')
    scope = dict(os=os, csv=csv, fcntl=fcntl, logger=logging.getLogger('fixture'), generate_error_report=lambda *a:None)
    exec(compile(ast.Module(body=[function],type_ignores=[]),'<patched-csv>','exec'),scope)
    result = {'device_id':device, 'result':{'case_statistics':[{'case':{'dtype':'float32'},'run_count':1,'result':True}]}}
    scope['generate_csv_report']([result],output)


def spawn_probe(q, cpu):
    if cpu is not None:
        os.sched_setaffinity(0, {cpu})
    model = importlib.util.find_spec('ascend_npu_burn.benchmarks.model')
    from ascend_npu_burn import runtime_compat
    q.put((model.origin, runtime_compat.__file__,
           {k:os.getenv(k) for k in ('OPENBLAS_NUM_THREADS','OMP_NUM_THREADS','MKL_NUM_THREADS','NUMEXPR_NUM_THREADS','BLIS_NUM_THREADS')},
           sorted(os.sched_getaffinity(0))))


class PortableTest(unittest.TestCase):
    def test_stream_sync_success_failure_and_timeout(self):
        # Exercise the applied patch without importing torch or probing NPU.
        source = (Path(compat.__file__).parent/'common/thread.py').read_text()
        functions = [n for n in ast.parse(source).body if isinstance(n, ast.FunctionDef)
                     and n.name in ('stream_sync', 'stream_sync_timeout')]
        scope = dict(os=os, threading=threading, logger=logging.getLogger('fixture'))
        exec(compile(ast.Module(body=functions, type_ignores=[]), '<patched-sync>', 'exec'), scope)
        sync = scope['stream_sync_timeout']
        self.assertFalse(sync(types.SimpleNamespace(synchronize=lambda: None), 1))
        error = RuntimeError('CANN compilation failed: read-only kernel_meta')
        def fail():
            raise error
        with self.assertRaises(RuntimeError) as caught:
            sync(types.SimpleNamespace(synchronize=fail), 1)
        self.assertIs(caught.exception, error)
        # Some composite operators call synchronization inside OpThread.
        # Its join must propagate the failure too, not return an empty PASS.
        cls = next(n for n in ast.parse(source).body if isinstance(n, ast.ClassDef) and n.name == 'OpThread')
        scope['SDCDetect'] = lambda: None
        exec(compile(ast.Module(body=[cls], type_ignores=[]), '<patched-opthread>', 'exec'), scope)
        worker = scope['OpThread'](None, None, 1, 0, None, None)
        worker._run = lambda: sync(types.SimpleNamespace(synchronize=fail), 1)
        worker.start()
        with self.assertRaises(RuntimeError) as caught:
            worker.join(2)
        self.assertIs(caught.exception, error)
        self.assertFalse(worker.is_alive())
        release, finished = threading.Event(), threading.Event()
        def blocked():
            release.wait(5)
            finished.set()
        try:
            self.assertTrue(sync(types.SimpleNamespace(synchronize=blocked), 0.01))
        finally:
            release.set()
            self.assertTrue(finished.wait(2), 'fixture thread leaked')

    def test_full_config_and_generation_isolation(self):
        from ascend_npu_burn.case_catalog import load_config, catalog, select_cases
        base = load_config('A5')['cases'][0]
        a3 = load_config('A3')['cases'][0]
        self.assertEqual(len(base), 20)
        self.assertEqual(set(a3), set(base))
        self.assertEqual(a3['matmul'], base['matmul'])
        self.assertEqual(a3['quant_matmul']['dtype'], ['int32','int8'])
        self.assertNotIn('MKN', a3['quant_matmul'])
        self.assertEqual(select_cases('A3','matmul,quant_matmul',''), ['matmul','quant_matmul'])
        self.assertEqual(select_cases('A3','','default'), ['matmul','quant_matmul','weight_quant_batchmatmul'])
        for cases, group in [('missing',''), ('matmul,matmul',''), ('gpt3',''), ('','group_basic')]:
            with self.assertRaises(ValueError): select_cases('A3', cases, group)
        # Exercise the real ConfigManager merge without importing hardware.
        source = (Path(compat.__file__).parent/'common/utils.py').read_text()
        manager = next(n for n in ast.parse(source).body if isinstance(n,ast.ClassDef) and n.name=='ConfigManager')
        init = next(n for n in manager.body if isinstance(n,ast.FunctionDef) and n.name=='__init__')
        init.args.args[1].annotation = init.args.args[2].annotation = None
        import copy
        scope = dict(json=json, copy=copy)
        exec(compile(ast.Module(body=[init],type_ignores=[]),'<config>','exec'),scope)
        holder = types.SimpleNamespace(_parse_config=lambda: None)
        root = Path(compat.__file__).parent/'config'
        scope['__init__'](holder, str(root/'base_config.json'), str(root/'a3_config.json'))
        self.assertEqual(holder.raw_data, load_config('A3'))
        if (root/'a2_config.json').exists():
            a2 = load_config('A2')['cases'][0]
            self.assertEqual(set(a2),set(base))
            self.assertEqual(a2['matmul']['run_count'],20)
            self.assertEqual(a3['matmul']['run_count'],100)
            self.assertEqual(a2['conv2d'], base['conv2d'])
            with self.assertRaisesRegex(ValueError,'no upstream A2'):
                select_cases('A2','quant_matmul','')
        elif 'A2 = "A2"' in (root.parent/'common/enum.py').read_text():
            # Full-base profile must not silently inherit the sizing profile.
            self.assertEqual(load_config('A2'), load_config('A5'))
            self.assertEqual(base['matmul']['run_count'], 100)
            self.assertEqual(len(base['matmul']['dtype']), 5)
            self.assertEqual(len(base['matmul']['shape']), 7)
            self.assertEqual(len(base['matmul']['pattern']), 6)
            scope['__init__'](holder, str(root/'base_config.json'), None)
            self.assertEqual(holder.raw_data, load_config('A2'))
            self.assertNotIn('data_load.data_size = 1', (root.parent/'runtime/engine.py').read_text())
            with self.assertRaisesRegex(ValueError, 'no upstream A2'):
                select_cases('A2', 'quant_matmul', '')
        supported = {'float32','float16','float','bfloat16','double','int64','int32','int16','int8','uint8','bool'}
        items = {i['name']: i for i in catalog('A3', supported_dtypes=supported)}
        effective = {name: config for group in load_config('A3')['cases'] for name, config in group.items()}
        self.assertEqual(items['matmul']['config'], effective['matmul'])
        self.assertEqual(items['conv2d']['config'], effective['conv2d'])
        items['matmul']['config']['run_count'] = -1
        self.assertEqual({i['name']: i for i in catalog('A3')}['matmul']['config'], effective['matmul'])
        self.assertTrue(items['matmul']['selectable'])
        self.assertTrue(items['quant_matmul']['selectable'])
        self.assertTrue(all(i['hardware_validation']=='NOT_RUN' for i in items.values()))
        with self.assertRaisesRegex(ValueError,'unsupported dtype'):
            select_cases('A5','quant_matmul','',supported_dtypes=supported)

    def test_dtype_capabilities(self):
        required = 'float64 float32 float16 float bfloat16 double int64 int32 int16 int8 uint8 bool'.split()
        for optional in (False, True):
            torch = types.ModuleType('torch')
            npu = types.ModuleType('torch_npu')
            for name in required:
                setattr(torch, name, object())
            if optional:
                torch.float8_e4m3fn = object()
                npu.float4_e2m1fn_x2 = object()
                npu.hifloat8 = object()
            sys.modules['torch'] = torch
            sys.modules['torch_npu'] = npu
            sys.modules.pop('ascend_npu_burn.common.const', None)
            const = importlib.import_module('ascend_npu_burn.common.const')
            self.assertIs(const.DTYPE_MAPPING['float32'], torch.float32)
            for name in ('mxfloat4', 'float8', 'hifloat8'):
                if optional:
                    self.assertIn(name, const.DTYPE_MAPPING)
                else:
                    with self.assertRaisesRegex(ValueError, 'Unsupported dtype'):
                        const.DTYPE_MAPPING[name]
        sys.modules.pop('torch', None)
        sys.modules.pop('torch_npu', None)

    def test_cpu_lists_socket_unknown_sparse_offline(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/'devices/system/cpu').mkdir(parents=True)
            (root/'devices/system/cpu/online').write_text('0-3,8,10-11')
            for node, cpus in {2:'0-4,8',7:'9-12'}.items():
                p=root/f'devices/system/node/node{node}'
                p.mkdir(parents=True)
                (p/'cpulist').write_text(cpus)
            # Unknown Socket(s) is irrelevant: no arithmetic based on lscpu.
            got=compat.numa_cpus(root, {0,1,8,9,10})
            self.assertEqual(got, {2:[0,1,8],7:[10]})
            self.assertEqual(compat.binding_cpus(got,2,{0}),[0])
            with self.assertRaisesRegex(RuntimeError,'Selected NUMA'):
                compat.binding_cpus(got,7,{0})
            for bad in ('0-2,x', '4-1', '-'):
                with self.assertRaises(ValueError): compat.cpu_list(bad)

    def test_single_device_does_not_use_first_pci(self):
        pci={'0000:01:00.0':0, '0000:81:00.0':7}
        nodes={'14':'0000:81:00.0'}
        self.assertEqual(compat.select_topology(pci,nodes,1,'single','0',1,{0:'0000:81:00.0'}),{7:[0]})
        for arguments in [
            (pci,nodes,2,'single','0',1),
            (pci,nodes,1,'single','14',1),
            (pci,nodes,1,'single','all',1),
            (pci,{'14':None},1,'single','0',1),
            ({'0000:81:00.0':-1},nodes,1,'single','0',1),
            (pci,nodes,1,'full','0',1),
            (pci,nodes,1,'single','0',1), # counts alone are not proof
            (pci,nodes,1,'single','0',1,{0:'0000:01:00.0'}),
        ]:
            with self.assertRaises(RuntimeError): compat.select_topology(*arguments)

    def test_runtime_pci_api_capability_and_failure(self):
        class Query:
            def __init__(self, status=0): self.status=status
            def __call__(self, index, buffer, length):
                if index != 0 or length < 13: raise AssertionError('invalid CANN query contract')
                buffer.value=b'0000:81:00.0'
                return self.status
        library=types.SimpleNamespace(aclrtDeviceGetPCIBusId=Query())
        self.assertEqual(compat.runtime_pci_bdf(0,library),'0000:81:00.0')
        for bad in (types.SimpleNamespace(),types.SimpleNamespace(aclrtDeviceGetPCIBusId=Query(1))):
            with self.assertRaisesRegex(RuntimeError,'UNVERIFIED'):
                compat.runtime_pci_bdf(0,bad)

    def test_process_cpuset_not_root_cpuset(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/'cpuset.cpus.effective').write_text('0-15')
            (root/'job').mkdir()
            (root/'job/cpuset.cpus.effective').write_text('2,5-6')
            membership = root/'membership'
            membership.write_text('0::/job\n')
            self.assertEqual(compat.effective_cpuset(root, membership), {2,5,6})

    def test_full_sparse_nodes_and_sixteen(self):
        pci={f'0000:{i:02x}:00.0': i//8 for i in range(16)}
        nodes={str(i+2):None for i in range(16)}
        self.assertEqual(compat.select_topology(pci,nodes,16,'full','14,15',16),
                         {0:list(range(8)),1:list(range(8,16))})

    def test_namespace_and_spawn_inherit(self):
        fake=types.ModuleType('benchmarks')
        fake.__path__=['/does/not/exist/old-install/benchmarks']
        sys.modules['benchmarks']=fake
        model = importlib.util.find_spec('ascend_npu_burn.benchmarks.model')
        self.assertIn('ascend_npu_burn/benchmarks/model',model.origin)
        for k in ('OPENBLAS_NUM_THREADS','OMP_NUM_THREADS','MKL_NUM_THREADS','NUMEXPR_NUM_THREADS','BLIS_NUM_THREADS'):
            os.environ[k]='1'
        ctx=mp.get_context('spawn')
        q=ctx.Queue()
        affinity=sorted(os.sched_getaffinity(0))
        p=ctx.Process(target=spawn_probe,args=(q,affinity[0]))
        p.start()
        try:
            origin, helper, threads, child_affinity=compat.receive(q,[p],time.monotonic()+10)
            self.assertEqual(origin,model.origin)
            self.assertEqual(helper,compat.__file__)
            self.assertEqual(set(threads.values()),{'1'})
            self.assertEqual(child_affinity,[affinity[0]])
            self.assertEqual(sorted(os.sched_getaffinity(0)),affinity)
        finally:
            compat.reap([p])
            q.close()

    def test_failed_start_propagates_and_reaps_sibling(self):
        ctx=mp.get_context('spawn')
        q=ctx.Queue()
        processes=[ctx.Process(target=fail_worker),ctx.Process(target=sleep_worker)]
        for p in processes: p.start()
        start=time.monotonic()
        try:
            with self.assertRaisesRegex(RuntimeError,'Worker.*exited'):
                compat.receive(q,processes,time.monotonic()+5)
        finally:
            compat.reap(processes)
            q.close()
        self.assertLess(time.monotonic()-start,6)
        self.assertTrue(all(not p.is_alive() and p.exitcode is not None for p in processes))

    def test_wait_timeout(self):
        with self.assertRaises(TimeoutError):
            compat.receive(queue.Queue(),[],time.monotonic()+0.05)

    def test_worker_exit_respects_job_deadline(self):
        class FinalizingWorker:
            pid = 123
            exitcode = None
            remaining = 3
            def is_alive(self):
                return self.remaining > 0
            def join(self, timeout):
                self.remaining -= 1
                if self.remaining <= 0:
                    self.exitcode = 0
        worker = FinalizingWorker()
        compat.join_success([worker], time.monotonic() + 2)
        self.assertEqual(worker.exitcode, 0)
        with self.assertRaisesRegex(TimeoutError, 'exit deadline'):
            compat.join_success([FinalizingWorker()], time.monotonic() - 1)
        worker.remaining, worker.exitcode = 0, 7
        # A finished process's zero-time join must preserve its exit status.
        worker.join = lambda timeout: None
        with self.assertRaisesRegex(RuntimeError, 'code 7'):
            compat.join_success([worker], time.monotonic() + 2)

    def test_reap_partial_start_and_independent_job_timeout(self):
        ctx=mp.get_context('spawn')
        not_started=ctx.Process(target=sleep_worker)
        compat.reap([not_started])
        old=os.environ.get('CATMONITOR_NPU_JOB_TIMEOUT_SECONDS')
        try:
            os.environ['CATMONITOR_NPU_JOB_TIMEOUT_SECONDS']='1800'
            self.assertEqual(compat.job_timeout(),1800)
            os.environ['CATMONITOR_NPU_JOB_TIMEOUT_SECONDS']='0'
            with self.assertRaises(ValueError): compat.job_timeout()
        finally:
            if old is None: os.environ.pop('CATMONITOR_NPU_JOB_TIMEOUT_SECONDS',None)
            else: os.environ['CATMONITOR_NPU_JOB_TIMEOUT_SECONDS']=old

    def test_concurrent_engine_csv_has_one_header(self):
        with tempfile.TemporaryDirectory() as output:
            ctx = mp.get_context('spawn')
            workers = [ctx.Process(target=csv_worker,args=(output,i)) for i in range(4)]
            for p in workers: p.start()
            try:
                for p in workers:
                    p.join(10)
                    self.assertEqual(p.exitcode,0)
                with open(Path(output)/'npu_burn_results.csv') as stream:
                    rows = list(csv.DictReader(stream))
                self.assertEqual(len(rows),4)
                self.assertEqual({row['device_id'] for row in rows},{'0','1','2','3'})
                self.assertTrue(all(row['result']=='PASS' for row in rows))
            finally:
                compat.reap(workers)

    def test_engine_uses_bounded_waits_and_finally(self):
        root=Path(compat.__file__).parent
        engine=(root/'runtime/engine.py').read_text()
        main=(root/'npu_burn.py').read_text()
        self.assertNotIn('result_queue_worker.get()',engine)
        self.assertNotIn('result_queue.get()',main)
        self.assertIn('finally:\n        reap(processes)',engine)
        self.assertIn('reap(engine_processes)',main)
        self.assertIn('ready_deadline',engine)
        self.assertIn('join_success(processes, deadline)',engine)
        self.assertNotIn('p.join(timeout=5)',engine)
        self.assertIn('join_success(engine_processes, deadline)',main)
        self.assertNotIn('process.join(timeout=5)',main)
        self.assertIn('thread.TIMEOUT = max(1, args.timeout)',engine)
        self.assertNotIn('mp.spawn(',main)
        self.assertNotIn('sys.path.append',main)


if __name__=='__main__':
    unittest.main()
