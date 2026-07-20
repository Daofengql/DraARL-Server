# 测试工具

- `bench/udp_fanout/`：Go 编写的 UDP fan-out 性能基准工具。
- `simulator/`：Python 编写的设备模拟与调试客户端。

Python 模拟客户端从仓库根目录启动：

```bash
python test/simulator/main.py
```

依赖安装：

```bash
python -m pip install -r test/simulator/requirements.txt
```
