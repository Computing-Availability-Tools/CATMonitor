#!/usr/bin/env python3
"""
XMind 测试用例思维导图生成器

根据测试点结构化数据，生成 XMind 格式的思维导图文件。

输入: JSON 格式的测试点数据（通过命令行参数指定 JSON 文件路径）
输出: .xmind 文件（XMind 8+ 兼容格式）

用法:
    python generate_xmind.py <input.json> [output.xmind]

JSON 输入格式:
{
    "title": "项目名称 - 测试点",
    "user_stories": [
        {
            "id": "US-001",
            "title": "用户故事标题",
            "description": "用户故事描述"
        }
    ],
    "categories": [
        {
            "name": "功能测试",
            "test_points": [
                {
                    "id": "TC-001",
                    "description": "测试点描述",
                    "user_story_id": "US-001",
                    "priority": "P0",
                    "sub_points": ["子测试点1", "子测试点2"]
                }
            ]
        }
    ]
}
"""

import json
import os
import sys
import uuid
import zipfile
from datetime import datetime
from pathlib import Path


def generate_id():
    """生成 XMind 节点 ID"""
    return str(uuid.uuid4())


def create_topic(text, priority=None, note_content=None, labels=None):
    """创建一个 XMind 主题节点"""
    topic = {
        "id": generate_id(),
        "class": "topic",
        "title": text,
        "structureClass": "org.xmind.ui.map.unbalanced"
    }

    # 添加优先级标记
    if priority:
        priority_map = {
            "P0": "priority-1",
            "P1": "priority-2",
            "P2": "priority-3"
        }
        markers = [{"markerId": priority_map.get(priority, "priority-3")}]

        # 如果有标签（如用户故事编号）
        if labels:
            for label in labels:
                markers.append({"markerId": "tag-red"})

        topic["markers"] = markers

    # 添加备注（用于存放详细信息）
    if note_content:
        topic["notes"] = {
            "plain": {
                "content": note_content
            }
        }

    # 添加标签
    if labels:
        topic["labels"] = labels

    return topic


def create_root_topic(title):
    """创建根主题"""
    return {
        "id": generate_id(),
        "class": "topic",
        "title": title,
        "structureClass": "org.xmind.ui.map.unbalanced",
        "children": {
            "attached": []
        }
    }


def add_child(parent, child_topic):
    """向父节点添加子节点"""
    if "children" not in parent:
        parent["children"] = {"attached": []}
    if "attached" not in parent["children"]:
        parent["children"]["attached"] = []
    parent["children"]["attached"].append(child_topic)


def build_xmind_content(data):
    """根据测试数据构建 XMind 内容 JSON"""
    root = create_root_topic(data.get("title", "测试点思维导图"))

    # 优先级图例节点
    legend_topic = create_topic("优先级图例")
    add_child(legend_topic, create_topic("P0 - 阻塞主流程，影响核心功能，无替代方案", priority="P0"))
    add_child(legend_topic, create_topic("P1 - 重要功能异常，有替代方案，影响用户体验", priority="P1"))
    add_child(legend_topic, create_topic("P2 - 边缘场景，UI细节，低概率异常", priority="P2"))
    add_child(root, legend_topic)

    # 用户故事索引节点
    user_stories = data.get("user_stories", [])
    if user_stories:
        us_index_topic = create_topic("用户故事索引")
        for us in user_stories:
            us_topic = create_topic(
                f"{us['id']}: {us['title']}",
                labels=[us["id"]],
                note_content=us.get("description", "")
            )
            add_child(us_index_topic, us_topic)
        add_child(root, us_index_topic)

    # 各分类测试点
    categories = data.get("categories", [])
    for category in categories:
        category_topic = create_topic(category["name"])

        test_points = category.get("test_points", [])
        for tp in test_points:
            # 主测试点节点
            tp_title = f"{tp['id']} | {tp['description']}"
            tp_topic = create_topic(
                tp_title,
                priority=tp.get("priority"),
                labels=[tp.get("user_story_id", "")] if tp.get("user_story_id") else None,
                note_content=f"优先级: {tp.get('priority', 'N/A')}\n关联用户故事: {tp.get('user_story_id', 'N/A')}\n分类: {category['name']}"
            )

            # 子测试点
            sub_points = tp.get("sub_points", [])
            for sp in sub_points:
                if isinstance(sp, str):
                    add_child(tp_topic, create_topic(sp))
                elif isinstance(sp, dict):
                    sp_topic = create_topic(
                        sp.get("description", ""),
                        priority=sp.get("priority"),
                        note_content=sp.get("detail", "")
                    )
                    # 子测试点的子节点
                    for ssp in sp.get("sub_points", []):
                        if isinstance(ssp, str):
                            add_child(sp_topic, create_topic(ssp))
                    add_child(tp_topic, sp_topic)

            add_child(category_topic, tp_topic)

        add_child(root, category_topic)

    # 构建完整 content
    content = [{
        "id": generate_id(),
        "class": "sheet",
        "title": data.get("title", "测试点"),
        "rootTopic": root
    }]

    return content


def build_meta():
    """构建 meta.json"""
    return {
        "creator": {
            "name": "WorkBuddy Test Case Generator",
            "version": "1.0.0"
        },
        "createTimes": int(datetime.now().timestamp() * 1000)
    }


def build_manifest():
    """构建 manifest.json"""
    return {
        "file-entries": {
            "content.json": {},
            "metadata.json": {}
        }
    }


def generate_xmind(data, output_path):
    """生成 XMind 文件"""
    content = build_xmind_content(data)
    meta = build_meta()
    manifest = build_manifest()

    # 确保输出目录存在
    output_dir = os.path.dirname(output_path)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)

    # 创建 XMind 文件（ZIP 格式）
    with zipfile.ZipFile(output_path, 'w', zipfile.ZIP_DEFLATED) as zf:
        zf.writestr('content.json', json.dumps(content, ensure_ascii=False, indent=2))
        zf.writestr('metadata.json', json.dumps(meta, ensure_ascii=False, indent=2))
        zf.writestr('manifest.json', json.dumps(manifest, ensure_ascii=False, indent=2))

    return output_path


def main():
    if len(sys.argv) < 2:
        print("用法: python generate_xmind.py <input.json> [output.xmind]")
        print("  input.json  - 测试点 JSON 数据文件")
        print("  output.xmind - 输出的 XMind 文件路径（可选，默认与输入同目录）")
        sys.exit(1)

    input_path = sys.argv[1]

    if not os.path.exists(input_path):
        print(f"错误: 输入文件不存在: {input_path}")
        sys.exit(1)

    # 确定输出路径
    if len(sys.argv) >= 3:
        output_path = sys.argv[2]
    else:
        base_name = Path(input_path).stem
        output_dir = Path(input_path).parent
        output_path = str(output_dir / f"{base_name}.xmind")

    # 读取输入数据
    with open(input_path, 'r', encoding='utf-8') as f:
        data = json.load(f)

    # 生成 XMind 文件
    result = generate_xmind(data, output_path)
    print(f"XMind 文件已生成: {result}")
    return result


if __name__ == "__main__":
    main()
