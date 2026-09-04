import { useEffect, useState } from "react";
import { Alert, Button, Input, Modal, Popover, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";
import Icon from "./icons";

type CurrentUser = components["schemas"]["CurrentUser"];

// 顶栏身份浮层（#199）：项目壳与项目列表壳共用——头像首字、显示名、用户名、修改密码、登出。
// #207 起「修改密码」并入个人中心，本组件只需改这一处。
export function IdentityMenu({ user, onLogout }: { user: CurrentUser; onLogout: () => void }) {
  // 修改密码入口挂在身份浮层里（S3）：改完后端会吊销本人其余会话。
  const [passwordOpen, setPasswordOpen] = useState(false);
  return (
    <>
      <Popover
        trigger="click"
        placement="bottomRight"
        content={
          <div className="identity-popover">
            <div className="identity-popover-head">
              <span className="avatar">{user.displayName.slice(0, 1)}</span>
              <span>
                <b>{user.displayName}</b>
                <small>{user.username}</small>
              </span>
            </div>
            <Button block style={{ marginBottom: 8 }} onClick={() => setPasswordOpen(true)}>
              修改密码
            </Button>
            <Button block onClick={onLogout}>
              登出
            </Button>
          </div>
        }
      >
        <button className="identity" type="button" aria-label="当前身份">
          <span className="avatar">{user.displayName.slice(0, 1)}</span>
          <span className="who">
            <b>{user.displayName}</b>
            <small>{user.username}</small>
          </span>
          <Icon name="down" size={15} />
        </button>
      </Popover>
      <ChangePasswordModal open={passwordOpen} onClose={() => setPasswordOpen(false)} />
    </>
  );
}

// ChangePasswordModal 修改本人登录密码（S3）：成功后本人其余会话立即失效，当前会话保留。
function ChangePasswordModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) {
      setCurrent("");
      setNext("");
      setConfirm("");
      setError(null);
    }
  }, [open]);

  const submit = async () => {
    if (next !== confirm) {
      setError("两次输入的新密码不一致");
      return;
    }
    setSaving(true);
    setError(null);
    const res = await client.POST("/auth/change-password", {
      body: { currentPassword: current, newPassword: next },
    });
    setSaving(false);
    if (res.response.ok) {
      message.success("密码已修改，本人其余会话已失效");
      onClose();
    } else {
      setError(res.error?.message ?? "修改失败");
    }
  };

  return (
    <Modal
      title="修改登录密码"
      open={open}
      okText="确认修改"
      cancelText="取消"
      confirmLoading={saving}
      okButtonProps={{ disabled: !current || [...next].length < 8 || [...next].length > 32 || !confirm }}
      onCancel={onClose}
      onOk={submit}
    >
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      <p className="muted" style={{ marginTop: 0 }}>
        新密码 8～32 位；修改成功后除当前浏览器外，本人其余登录会话会立即失效。
      </p>
      <Input.Password
        placeholder="当前密码"
        value={current}
        onChange={(e) => setCurrent(e.target.value)}
        style={{ marginBottom: 8 }}
      />
      <Input.Password
        placeholder="新密码（8～32 位）"
        value={next}
        maxLength={32}
        onChange={(e) => setNext(e.target.value)}
        style={{ marginBottom: 8 }}
      />
      <Input.Password
        placeholder="再次输入新密码"
        value={confirm}
        maxLength={32}
        onChange={(e) => setConfirm(e.target.value)}
      />
    </Modal>
  );
}
