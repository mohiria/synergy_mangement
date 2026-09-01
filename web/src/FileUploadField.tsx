import { useRef, useState } from "react";
import { Button, Modal, message } from "antd";

// 统一上传组件（AC-52）：点击或拖拽选择、待上传文件行、文件名点击预览、提交前删除。
// 组件只持有“本次选择”，真正的上传由调用方在确认提交时发起；
// 调用方关闭窗口时把 value 置空，未提交的选择即不保留。
// #120：multiple 模式一次选多个文件（files/onFilesChange 受控），单文件调用方不受影响。

const DEFAULT_ACCEPT = ".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.png,.jpg,.jpeg,.zip";
const DEFAULT_MAX_MB = 20;

export function formatFileSize(size = 0): string {
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(size / 1024))} KB`;
}

export function fileTypeLabel(fileName: string): string {
  const ext = fileName.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "pdf") return "PDF";
  if (["doc", "docx"].includes(ext)) return "Word";
  if (["xls", "xlsx", "csv"].includes(ext)) return "Excel";
  if (["ppt", "pptx"].includes(ext)) return "PowerPoint";
  if (["png", "jpg", "jpeg", "gif", "webp"].includes(ext)) return "图片";
  if (ext === "zip") return "ZIP";
  return "文件";
}

export default function FileUploadField({
  value,
  onChange,
  multiple,
  files,
  onFilesChange,
  prompt = "点击选择或将文件拖到此处",
  hint,
  accept = DEFAULT_ACCEPT,
  maxMb = DEFAULT_MAX_MB,
  disabled,
}: {
  value?: File | null;
  onChange?: (file: File | null) => void;
  multiple?: boolean;
  files?: File[];
  onFilesChange?: (files: File[]) => void;
  prompt?: string;
  hint?: string;
  accept?: string;
  maxMb?: number;
  disabled?: boolean;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [preview, setPreview] = useState<{ url: string; file: File } | null>(null);

  const selected = multiple ? (files ?? []) : value ? [value] : [];

  const pick = (picked: FileList | File[] | null | undefined) => {
    const list = Array.from(picked ?? []);
    if (list.length === 0) return;
    const kept: File[] = [];
    for (const file of list) {
      if (file.size > maxMb * 1024 * 1024) {
        message.error(`「${file.name}」超过 ${maxMb}MB，已跳过`);
        continue;
      }
      kept.push(file);
    }
    if (kept.length === 0) return;
    if (multiple) {
      onFilesChange?.([...(files ?? []), ...kept]);
    } else {
      onChange?.(kept[0]);
    }
  };
  const removeAt = (idx: number) => {
    if (multiple) {
      onFilesChange?.((files ?? []).filter((_, i) => i !== idx));
    } else {
      onChange?.(null);
    }
  };
  const openPreview = (file: File) => {
    setPreview({ url: URL.createObjectURL(file), file });
  };
  const closePreview = () => {
    if (preview) URL.revokeObjectURL(preview.url);
    setPreview(null);
  };

  const isImage = !!preview && preview.file.type.startsWith("image/");
  const isPdf =
    !!preview && (preview.file.type === "application/pdf" || /\.pdf$/i.test(preview.file.name));

  return (
    <div className="upload-field">
      <div
        className={`upload-zone${dragging ? " dragging" : ""}${selected.length > 0 ? " selected" : ""}`}
        role="button"
        tabIndex={disabled ? -1 : 0}
        aria-disabled={disabled}
        onClick={() => !disabled && inputRef.current?.click()}
        onKeyDown={(e) => {
          if (!disabled && (e.key === "Enter" || e.key === " ")) {
            e.preventDefault();
            inputRef.current?.click();
          }
        }}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          if (!disabled) pick(e.dataTransfer.files);
        }}
      >
        <strong>{prompt}</strong>
        <span>{hint ?? `支持 PDF、Office、图片或 ZIP，单个文件不超过 ${maxMb}MB`}</span>
        <input
          ref={inputRef}
          type="file"
          accept={accept}
          multiple={multiple}
          style={{ display: "none" }}
          onChange={(e) => {
            pick(e.target.files);
            e.target.value = "";
          }}
        />
      </div>
      {selected.map((file, idx) => (
        <div className="upload-file-row" key={`${file.name}-${idx}`}>
          <div>
            <button type="button" className="upload-file-link" onClick={() => openPreview(file)}>
              {file.name}
            </button>
            <span>
              {formatFileSize(file.size)} · {fileTypeLabel(file.name)} · 已准备上传
            </span>
          </div>
          <Button size="small" disabled={disabled} onClick={() => removeAt(idx)}>
            删除
          </Button>
        </div>
      ))}
      <Modal
        title="文件预览"
        open={!!preview}
        width={860}
        onCancel={closePreview}
        footer={
          <Button type="primary" onClick={closePreview}>
            返回上传
          </Button>
        }
      >
        {preview && (
          <>
            <p className="muted" style={{ marginTop: 0 }}>
              {preview.file.name} · {formatFileSize(preview.file.size)}
            </p>
            {isImage && (
              <img className="local-preview-image" src={preview.url} alt={preview.file.name} />
            )}
            {!isImage && isPdf && (
              <iframe className="local-preview-frame" src={preview.url} title={preview.file.name} />
            )}
            {!isImage && !isPdf && (
              <div className="local-preview-fallback">
                <b>{preview.file.name}</b>
                <p>
                  {fileTypeLabel(preview.file.name)} 文件暂不支持在线预览，可先下载确认；
                  <br />
                  提交后该文件才形成业务事实。
                </p>
                <Button
                  onClick={() => {
                    const a = document.createElement("a");
                    a.href = preview.url;
                    a.download = preview.file.name;
                    a.click();
                  }}
                >
                  下载查看
                </Button>
              </div>
            )}
          </>
        )}
      </Modal>
    </div>
  );
}
