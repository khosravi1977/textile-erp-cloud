import os
from pathlib import Path
import sys

# پوشه‌هایی که باید نادیده گرفته شوند
IGNORE_DIRS = {
    '__pycache__', '.git', '.svn', '.hg',
    '.venv', 'venv', 'env', 'ENV',
    'node_modules', '.idea', '.vscode',
    'dist', 'build', 'target', '.pytest_cache',
    '.mypy_cache', '.tox', 'htmlcov', 'coverage',
    '.next', '.nuxt', 'out', '.cache'
}

# فایل‌هایی که باید نادیده گرفته شوند
IGNORE_FILES = {
    '.DS_Store', 'Thumbs.db', '.gitignore', '.gitattributes',
    '.env', '.env.local', '.env.production',
    'package-lock.json', 'yarn.lock', 'poetry.lock', 'Pipfile.lock',
    'project_code.txt'
}

# پسوندهای فایل‌های باینری
BINARY_EXTENSIONS = {
    '.pyc', '.pyo', '.pyd', '.so', '.dll', '.dylib', '.exe',
    '.jpg', '.jpeg', '.png', '.gif', '.bmp', '.ico', '.svg',
    '.mp3', '.mp4', '.avi', '.mov', '.wav', '.flac',
    '.zip', '.tar', '.gz', '.rar', '.7z',
    '.pdf', '.doc', '.docx', '.xls', '.xlsx',
    '.db', '.sqlite', '.sqlite3',
    '.woff', '.woff2', '.ttf', '.eot', '.otf'
}

def should_ignore(path, base_path):
    try:
        rel_path = path.relative_to(base_path)
        parts = rel_path.parts
        
        for part in parts:
            if part in IGNORE_DIRS or part.startswith('.'):
                return True, f"پوشه نادیده گرفته شده: {part}"
        
        if path.name in IGNORE_FILES:
            return True, f"فایل خاص: {path.name}"
        
        if path.suffix.lower() in BINARY_EXTENSIONS:
            return True, f"فایل باینری: {path.suffix}"
        
        return False, None
    except Exception as e:
        return True, f"خطا: {str(e)}"

def build_tree_structure(project_path):
    """ساخت ساختار درختی پروژه"""
    tree_lines = []
    project_path = Path(project_path).resolve()
    
    def add_tree_item(path, prefix="", is_last=True):
        if path.is_file():
            should_skip, _ = should_ignore(path, project_path)
            if should_skip:
                return
            connector = "└── " if is_last else "├── "
            tree_lines.append(f"{prefix}{connector}{path.name}")
        elif path.is_dir():
            # بررسی اینکه آیا پوشه باید نادیده گرفته شود
            if path.name in IGNORE_DIRS or path.name.startswith('.'):
                return
            
            connector = "└── " if is_last else "├── "
            tree_lines.append(f"{prefix}{connector}{path.name}/")
            
            # لیست محتویات پوشه
            try:
                children = sorted(path.iterdir(), key=lambda x: (x.is_file(), x.name))
                children = [c for c in children if not (c.name in IGNORE_DIRS or c.name.startswith('.'))]
                
                for i, child in enumerate(children):
                    is_last_child = (i == len(children) - 1)
                    extension = "    " if is_last else "│   "
                    add_tree_item(child, prefix + extension, is_last_child)
            except PermissionError:
                pass
    
    tree_lines.append(f"{project_path.name}/")
    try:
        children = sorted(project_path.iterdir(), key=lambda x: (x.is_file(), x.name))
        children = [c for c in children if not (c.name in IGNORE_DIRS or c.name.startswith('.'))]
        
        for i, child in enumerate(children):
            is_last_child = (i == len(children) - 1)
            add_tree_item(child, "", is_last_child)
    except PermissionError:
        tree_lines.append("خطا: دسترسی به پوشه امکان‌پذیر نیست")
    
    return tree_lines

def collect_project_files(project_path, output_file):
    project_path = Path(project_path).resolve()
    
    if not project_path.exists():
        print(f"خطا: مسیر {project_path} وجود ندارد!")
        return
    
    collected_files = []
    skipped_files = []
    
    # ساخت ساختار درختی
    tree_structure = build_tree_structure(project_path)
    
    with open(output_file, 'w', encoding='utf-8') as out:
        # هدر اصلی
        out.write(f"{'='*40}\n")
        out.write(f"پروژه: {project_path.name}\n")
        out.write(f"مسیر: {project_path}\n")
        out.write(f"{'='*40}\n")
        
        # نقشه کلی پروژه
        out.write(f"\n{'='*40}\n")
        out.write("نقشه کلی پروژه:\n")
        out.write(f"{'-'*40}\n")
        for line in tree_structure:
            out.write(f"{line}\n")
        out.write(f"{'='*40}\n")
        
        # محتوای فایل‌ها
        for file_path in sorted(project_path.rglob('*')):
            if not file_path.is_file():
                continue
            
            should_skip, reason = should_ignore(file_path, project_path)
            
            if should_skip:
                skipped_files.append((file_path, reason))
                continue
            
            try:
                rel_path = file_path.relative_to(project_path)
                parent_dir = rel_path.parent if rel_path.parent != Path('.') else 'ریشه'
                
                # هدر فایل
                out.write(f"\n{'='*40}\n")
                out.write(f"پوشه: {parent_dir}\n")
                out.write(f"فایل: {file_path.name}\n")
                out.write(f"پسوند: {file_path.suffix or 'ندارد'}\n")
                out.write(f"{'-'*40}\n")
                
                # محتوای فایل
                content = file_path.read_text(encoding='utf-8')
                out.write(content.rstrip())
                out.write('\n')
                
                collected_files.append(file_path)
                
            except Exception as e:
                skipped_files.append((file_path, f"خطا در خواندن: {str(e)}"))
        
        # خلاصه نهایی
        out.write(f"\n{'='*40}\n")
        out.write(f"جمع‌آوری شده: {len(collected_files)} فایل\n")
        out.write(f"نادیده گرفته شده: {len(skipped_files)} فایل\n")
        out.write(f"{'='*40}\n")
    
    print(f"\n✓ فایل {output_file} با موفقیت ایجاد شد")
    print(f"  - {len(collected_files)} فایل جمع‌آوری شد")
    print(f"  - {len(skipped_files)} فایل نادیده گرفته شد")

if __name__ == "__main__":
    project_path = sys.argv[1] if len(sys.argv) > 1 else "."
    output_file = "project_code.txt"
    collect_project_files(project_path, output_file)
