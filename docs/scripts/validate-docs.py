#!/usr/bin/env python3
import json
import pathlib
import sys

def main():
    docs_dir = pathlib.Path(__file__).parent.parent
    docs_json_path = docs_dir / "docs.json"
    
    # 1. Parse JSON
    try:
        with open(docs_json_path) as f:
            config = json.load(f)
    except Exception as e:
        print(f"Error parsing docs.json: {e}", file=sys.stderr)
        sys.exit(1)
        
    # 2. Extract pages from navigation
    nav = config.get("navigation", {})
    pages = []
    
    def walk(obj):
        if isinstance(obj, dict):
            for k, v in obj.items():
                if k == "pages":
                    pages.extend(v)
                else:
                    walk(v)
        elif isinstance(obj, list):
            for x in obj:
                walk(x)
                
    walk(nav)
    
    # 3. Check file existence
    missing = []
    for p in pages:
        mdx_file = docs_dir / f"{p}.mdx"
        if not mdx_file.is_file():
            missing.append(p)
            
    if missing:
        print(f"Error: Missing MDX pages:\n" + "\n".join(f"  - docs/{m}.mdx" for m in missing), file=sys.stderr)
        sys.exit(1)
        
    print(f"Validation OK: All {len(pages)} pages resolved successfully.")

if __name__ == "__main__":
    main()
