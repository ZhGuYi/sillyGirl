package qq

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/v2/adapter/logs"

	"github.com/cdle/sillyGirl/develop/core"
	"github.com/cdle/sillyGirl/utils"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var qq = core.MakeBucket("qq")

type Result struct {
	Retcode int `json:"retcode"`
	Data    struct {
		MessageID interface{} `json:"message_id"`
	} `json:"data"`
	Status string      `json:"status"`
	Error  interface{} `json:"error"`
	Echo   string      `json:"echo"`
}

type CallApi struct {
	Action string                 `json:"action"`
	Echo   string                 `json:"echo"`
	Params map[string]interface{} `json:"params"`
}

type sender struct {
	Age      int    `json:"age"`
	Area     string `json:"area"`
	Card     string `json:"card"`
	Level    string `json:"level"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
	Sex      string `json:"sex"`
	Title    string `json:"title"`
	UserID   int    `json:"user_id"`
}

type Message struct {
	Anonymous   interface{} `json:"anonymous"`
	Font        int         `json:"font"`
	GroupID     int         `json:"group_id"`
	Message     string      `json:"message"`
	MessageID   interface{} `json:"message_id"`
	MessageType string      `json:"message_type"`
	PostType    string      `json:"post_type"`
	RawMessage  string      `json:"raw_message"`
	SelfID      int         `json:"self_id"`
	Sender      sender      `json:"sender"`
	SubType     string      `json:"sub_type"`
	Time        int         `json:"time"`
	UserID      int         `json:"user_id"`
}

var conns = map[string]*QQ{}
var defaultBot = ""
var ignore string

type QQ struct {
	conn *websocket.Conn
	sync.RWMutex
	id    int
	chans map[string]chan string
}

func (qq *QQ) WriteJSON(ca CallApi) (string, error) {
	qq.Lock()
	qq.id++
	ca.Echo = fmt.Sprint(qq.id)
	cy := make(chan string, 1)
	defer close(cy)
	qq.chans[ca.Echo] = cy
	if err := qq.conn.WriteJSON(ca); err != nil {
		qq.Unlock()
		return "", err
	}
	qq.Unlock()
	select {
	case v := <-cy:
		return v, nil
	case <-time.After(time.Second * 60):
	}
	return "", nil
}

func init() {
	core.OttoFuncs["qq_bots"] = func(string) string {
		ss := []string{}
		for v := range conns {
			ss = append(ss, v)
		}
		return strings.Join(ss, " ")
	}
	ignore = qq.GetString("ignore")
	go func() {
		core.Server.GET("/qq/receive", func(c *gin.Context) {
			auth := c.GetHeader("Authorization")
			token := qq.GetString("access_token")
			if token == "" {
				token = qq.GetString("token")
			}
			// if token == "" {
			// 	logs.Warn("Onebot token is required!")
			// 	c.String(200, "Onebot token is required! 如果你看到这条消息说明你不瞎，新版要求在第三方QQ客户端设置access_token， 同时执行 set qq token $access_token")
			// 	return
			// }
			if token != "" && !strings.Contains(auth, token) {
				logs.Warn("Onebot机器人access_token不正确，小心有人攻击你的傻妞！！！")
			}
			if token == "" {
				logs.Warn(`你需要在Onebot机器人配置access_token以及在傻妞配置对应的参数(set qq access_token ?)才能保证连接安全，如果不设置将会造成信息泄露和资产损失！！！`)
			}
			var upGrader = websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}
			ws, err := upGrader.Upgrade(c.Writer, c.Request, nil)
			if err != nil {
				c.Writer.Write([]byte(err.Error()))
				return
			}
			botID := c.GetHeader("X-Self-ID")
			if len(conns) == 0 {
				defaultBot = botID
			} else if qq.GetString("default_bot") == botID {
				defaultBot = botID
			}
			qqcon := &QQ{
				conn:  ws,
				chans: make(map[string]chan string),
			}

			conns[botID] = qqcon
			if !strings.Contains(ignore, botID) {
				ignore += "&" + botID
			}
			logs.Info("QQ机器人(%s)已连接。", botID)
			core.Pushs["qq"] = func(i interface{}, s string, _ interface{}, botID string) {
				if qq.GetBool("ban_one2one") && !strings.Contains(qq.GetString("masters"), fmt.Sprint(i)) {
					return
				}
				if botID == "" {
					botID = defaultBot
				}
				conn, ok := conns[botID]
				if !ok {
					botID = ""
					for v := range conns {
						conn = conns[v]
						botID = v
						break
					}
					if botID == "" {
						return
					}
				}
				s = strings.Trim(s, "\n")
				conn.WriteJSON(CallApi{
					Action: "send_private_msg",
					Params: map[string]interface{}{
						"user_id": utils.Int64(i),
						"message": s,
					},
				})
			}
			core.GroupPushs["qq"] = func(i, j interface{}, s string, botID string) {
				if botID == "" {
					botID = defaultBot
				}
				conn, ok := conns[botID]
				if !ok {
					botID = ""
					for v := range conns {
						conn = conns[v]
						botID = v
						break
					}
					if botID == "" {
						return
					}
				}
				userId := utils.Int64(j)
				if userId != 0 {
					if strings.Contains(s, "\n") {
						s = fmt.Sprintf(`[CQ:at,qq=%d]`, userId) + "\n" + s
					} else {
						s = fmt.Sprintf(`[CQ:at,qq=%d]`, userId) + s
					}
				}
				s = strings.Trim(s, "\n")
				conn.WriteJSON(CallApi{
					Action: "send_group_msg",
					Params: map[string]interface{}{
						"group_id": utils.Int(i),
						"user_id":  userId,
						"message":  s,
					},
				})
			}
			// var closed bool
			// go func() {
			for {
				_, data, err := ws.ReadMessage()
				// fmt.Println(string(data))
				if err != nil {
					ws.Close()
					logs.Info("QQ机器人(%s)已断开。", botID)
					delete(conns, botID)
					if defaultBot == botID {
						defaultBot = ""
						for v := range conns {
							defaultBot = v
							break
						}
					}
					break
				}
				{
					res := &Result{}
					json.Unmarshal(data, res)
					if res.Echo != "" {
						qqcon.RLock()
						if c, ok := qqcon.chans[res.Echo]; ok {
							c <- fmt.Sprint(res.Data.MessageID)
						}
						qqcon.RUnlock()
						continue
					}
				}
				msg := &Message{}
				json.Unmarshal(data, msg)
				if msg.MessageType != "private" && fmt.Sprint(msg.SelfID) != defaultBot {
					continue
				}
				// fmt.Println(msg)
				if msg.SelfID == msg.UserID {
					continue
				}
				if strings.Contains(ignore, fmt.Sprint(msg.UserID)) {
					continue
				}
				// if msg.PostType == "message" {
				msg.RawMessage = strings.ReplaceAll(msg.RawMessage, "\\r", "\n")
				msg.RawMessage = regexp.MustCompile(`[\n\r]+`).ReplaceAllString(msg.RawMessage, "\n")
				core.Senders <- &Sender{
					Conn:    qqcon,
					Message: msg,
				}
				// }
			}
			// closed = true
			// }()
			// for {
			// 	if closed {
			// 		break
			// 	}
			// 	qqcon.WriteJSON(CallApi{
			// 		Action: "get_status",
			// 	})
			// 	time.Sleep(time.Second)
			// }
		})
	}()
}

type Sender struct {
	botID    string
	Conn     *QQ
	Message  *Message
	matches  [][]string
	Duration *time.Duration
	deleted  bool
	core.BaseSender
}

func (sender *Sender) GetContent() string {
	if sender.Content != "" {
		return sender.Content
	}
	text := sender.Message.RawMessage
	text = strings.Replace(text, "amp;", "", -1)
	text = strings.Replace(text, "&#91;", "[", -1)
	text = strings.Replace(text, "&#93;", "]", -1)

	return strings.Trim(text, " ")
}

func (sender *Sender) GetUserID() string {
	return fmt.Sprint(sender.Message.UserID)
}

func (sender *Sender) GetChatID() int {
	return sender.Message.GroupID
}

func (sender *Sender) GetImType() string {
	return "qq"
}

func (sender *Sender) GetMessageID() string {
	return fmt.Sprint(sender.Message.MessageID)
}

func (sender *Sender) IsReply() bool {
	return false
}

func (sender *Sender) GetRawMessage() interface{} {
	return sender.Message
}

func (sender *Sender) IsAdmin() bool {
	if sender.Message.UserID == sender.Message.SelfID {
		return true
	}
	uid := fmt.Sprint(sender.Message.UserID)
	for _, v := range regexp.MustCompile(`\d+`).FindAllString(qq.GetString("masters"), -1) {
		if uid == v {
			return true
		}
	}
	return false
}

func (sender *Sender) IsMedia() bool {
	return false
}

func (sender *Sender) GroupKick(uid string, reject_add_request bool) {
	sender.Conn.WriteJSON(CallApi{
		Action: "set_group_kick",
		Params: map[string]interface{}{
			"group_id":           sender.Message.GroupID,
			"user_id":            utils.Int(uid),
			"reject_add_request": reject_add_request,
		},
	})
}

func (sender *Sender) GroupBan(uid string, duration int) {
	sender.Conn.WriteJSON(CallApi{
		Action: "set_group_ban",
		Params: map[string]interface{}{
			"group_id": sender.Message.GroupID,
			"user_id":  utils.Int(uid),
			"duration": duration,
		},
	})
}

var dd sync.Map

func (sender *Sender) Reply(msgs ...interface{}) ([]string, error) {
	if core.IsNoReplyGroup(sender) {
		return nil, nil
	}
	rt := ""
	for _, item := range msgs {
		switch item.(type) {
		case time.Duration:
			du := item.(time.Duration)
			sender.Duration = &du
		case error:
			rt = fmt.Sprint(item)
		case string:
			rt = item.(string)
		case []byte:
			rt = string(item.([]byte))
		case core.ImageUrl:
			rt = `[CQ:image,file=` + string(item.(core.ImageUrl)) + `]`
		case core.VideoUrl:
			rt = `[CQ:video,file=` + string(item.(core.VideoUrl)) + `]`
		}
	}
	if rt == "" {
		return []string{}, nil
	}
	rt = strings.Trim(rt, "\n")

	if sender.Atlast && !sender.IsFinished {
		sender.ToSendMessages = append(sender.ToSendMessages, rt)
		return []string{}, nil
	}

	ids := []string{}
	if sender.Message.MessageType == "private" {
		id, err := sender.Conn.WriteJSON(CallApi{
			Action: "send_private_msg",
			Params: map[string]interface{}{
				"user_id": sender.Message.UserID,
				"message": rt,
			},
		})
		ids = append(ids, id)
		return ids, err
	} else {
		id, err := sender.Conn.WriteJSON(CallApi{
			Action: "send_group_msg",
			Params: map[string]interface{}{
				"group_id": sender.Message.GroupID,
				"user_id":  sender.Message.UserID,
				"message":  rt,
			},
		})
		ids = append(ids, id)
		return ids, err
	}

}

func (sender *Sender) Delete() error {
	sender.Conn.WriteJSON(CallApi{
		Action: "delete_msg",
		Params: map[string]interface{}{
			"message_id": sender.Message.MessageID,
		},
	})
	return nil
}

func (sender *Sender) Disappear(lifetime ...time.Duration) {

}

func (sender *Sender) Copy() core.Sender {
	new := reflect.Indirect(reflect.ValueOf(interface{}(sender))).Interface().(Sender)
	return &new
}

func (sender *Sender) GetUsername() string {
	return sender.Message.Sender.Nickname
}

func (sender *Sender) RecallMessage(ps ...interface{}) error {
	for _, p := range ps {
		switch p.(type) {
		case string:
			sender.Conn.WriteJSON(CallApi{
				Action: "delete_msg",
				Params: map[string]interface{}{
					"message_id": p,
				},
			})
		case []string:
			for _, v := range p.([]string) {
				sender.Conn.WriteJSON(CallApi{
					Action: "delete_msg",
					Params: map[string]interface{}{
						"message_id": v,
					},
				})
			}
		}
	}
	return nil
}
