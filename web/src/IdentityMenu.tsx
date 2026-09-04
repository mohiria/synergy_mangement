import { Button, Popover } from "antd";
import { useNavigate } from "react-router-dom";
import type { components } from "./api/schema";
import Icon from "./icons";

type CurrentUser = components["schemas"]["CurrentUser"];

// 顶栏身份浮层（#199）：项目壳与项目列表壳共用——头像首字、显示名、用户名、个人中心、登出。
// #207：「修改密码」并入个人中心（/me/password），浮层不再直接改密。
export function IdentityMenu({ user, onLogout }: { user: CurrentUser; onLogout: () => void }) {
  const navigate = useNavigate();
  return (
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
          <Button block style={{ marginBottom: 8 }} onClick={() => navigate("/me/profile")}>
            个人中心
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
  );
}
