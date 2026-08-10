from benchmarks.op.fusion_attention_grad import FusionAttentionGradOp


def test_parse_cases():
    op = FusionAttentionGradOp()
    op.task_config = {"dtype": ["float16"], "BNSH": [[1, 16, 1024, 128]], "pattern": ["gauss_random"]}
    cases = op.parse_cases()

    assert len(cases) == 1
    assert cases[0]["B"] == 1
    assert cases[0]["N"] == 16
    assert cases[0]["S"] == 1024
    assert cases[0]["H"] == 128


def test_create_tensor_info():
    op = FusionAttentionGradOp()
    op.device = 0
    case = {"dtype": "float16", "B": 1, "N": 16, "S": 1024, "H": 128, "pattern": "gauss_random"}

    tensor_info = op.create_tensor_info(case)

    assert tensor_info["head_num"] == 16
    assert tensor_info["input_layout"] == "BNSD"

    assert tensor_info["q"].shape == [1, 16, 1024, 128]
    assert tensor_info["k"].shape == [1, 16, 1024, 128]
    assert tensor_info["v"].shape == [1, 16, 1024, 128]
