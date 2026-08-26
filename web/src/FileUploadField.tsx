import { useRef, useState } from "react";
import { Button, Modal, message } from "antd";

// 统一上传组件（AC-52）：点击或拖拽选择、待上传文件行、文件名点击预览、提交前删除。
// 组件只持有“本次选择”，真正的上传由调用方在确认提交时发起；
// 调用方关闭窗口时把 value 置空，未提交的选择即不保留。

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
  prompt = "点击选择或将文件拖到此处",
  hint,
  accept = DEFAULT_ACCEPT,
  maxMb = DEFAULT_MAX_MB,
  disabled,
}: {
  value: File | null;
  onChange: (file: File | null) => void;
  prompt?: string;
  hint?: string;
  accept?: string;
  maxMb?: number;
  disabled?: boolean;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [previewUrl, setPreviewUrl] = useState("");

  const pick = (file?: File | null) => {
    if (!file) return;
    if (file.size > maxMb * 1024 * 1024) {
      message.error(`文件不能超过 ${maxMb}MB`);
      return;
    }
    onChange(file);
  };
  const openPreview = () => {
    if (value) setPreviewUrl(URL.createObjectURL(value));
  };
  const closePreview = () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl);
    setPreviewUrl("");
  };

  const isImage = !!value && value.type.startsWith("image/");
  const isPdf = !!value && (value.type === "application/pdf" || /\.pdf$/i.test(value.name));

  return (
    <div className="upload-field">
      <div
        className={`upload-zone${dragging ? " dragging" : ""}${value ? " selected" : ""}`}
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
          if (!disabled) pick(e.dataTransfer.files?.[0]);
        }}
      >
        <strong>{prompt}</strong>
        <span>{hint ?? `支持 PDF、Office、图片或 ZIP，单个文件不超过 ${maxMb}MB`}</span>
        <input
          ref={inputRef}
          type="file"
          accept={accept}
          style={{ display: "none" }}
          onChange={(e) => {
            pick(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
      {value && (
        <div className="upload-file-row">
          <div>
            <button type="button" className="upload-file-link" onClick={openPreview}>
              {value.name}
            </button>
            <span>
              {formatFileSize(value.size)} · {fileTypeLabel(value.name)} · 已准备上传
            </span>
          </div>
          <Button size="small" disabled={disabled} onClick={() => onChange(null)}>
            删除
          </Button>
        </div>
      )}
      <Modal
        title="文件预览"
        open={!!previewUrl}
        width={860}
        onCancel={closePreview}
        footer={
          <Button type="primary" onClick={closePreview}>
            返回上传
          </Button>
        }
      >
        {value && (
          <>
            <p className="muted" style={{ marginTop: 0 }}>
              {value.name} · {formatFileSize(value.size)}
            </p>
            {isImage && <img className="local-preview-image" src={previewUrl} alt={value.name} />}
            {!isImage && isPdf && (
              <iframe className="local-preview-frame" src={previewUrl} title={value.name} />
            )}
            {!isImage && !isPdf && (
              <div className="local-preview-fallback">
                <b>{value.name}</b>
                <p>
                  {fileTypeLabel(value.name)} 文件暂不支持在线预览，可先下载确认；
                  <br />
                  提交后该文件才形成业务事实。
                </p>
                <Button
                  onClick={() => {
                    const a = document.createElement("a");
                    a.href = previewUrl;
                    a.download = value.name;
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
