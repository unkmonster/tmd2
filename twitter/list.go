package twitter

import (
	"fmt"
	"log"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
	"github.com/unkmonster/tmd2/internal/utils"
)

type ListBase interface {
	GetMembers(*resty.Client) ([]*User, error)
	GetId() int64
	Title() string
}

type List struct {
	Id          uint64
	MemberCount int
	Name        string
	Creator     *User
}

func GetLst(client *resty.Client, id uint64) (*List, error) {
	api := listByRestId{}
	api.id = id
	url := makeUrl(&api)

	resp, err := client.R().Get(url)
	if err != nil {
		return nil, err
	}

	err = utils.CheckRespStatus(resp)
	if err != nil {
		return nil, err
	}

	list := gjson.Get(resp.String(), "data.list")
	return parseList(&list)
}

func parseList(list *gjson.Result) (*List, error) {
	user_results := list.Get("user_results")
	creator, err := parseUserResults(&user_results)
	if err != nil {
		return nil, err
	}
	id_str := list.Get("id_str")
	member_count := list.Get("member_count")
	name := list.Get("name")

	result := List{}
	result.Creator = creator
	result.Id = id_str.Uint()
	result.MemberCount = int(member_count.Int())
	result.Name = name.String()
	return &result, nil
}

func itemContentsToUsers(itemContents []gjson.Result) []*User {
	users := make([]*User, 0, len(itemContents))
	for _, ic := range itemContents {
		user_results := getResults(ic, timelineUser)
		if user_results.String() == "{}" {
			continue
		}
		u, err := parseUserResults(&user_results)
		if err != nil {
			log.Printf("%v\n%v", err, user_results)
			continue
		}
		users = append(users, u)
	}
	return users
}

func getMembers(client *resty.Client, api timelineApi, instsPath string) ([]*User, error) {
	api.SetCursor("")
	itemContents, err := getTimelineItemContentsTillEnd(api, client, instsPath)
	if err != nil {
		return nil, err
	}
	return itemContentsToUsers(itemContents), nil
}

func (list *List) GetMembers(client *resty.Client) ([]*User, error) {
	api := listMembers{}
	api.count = 200
	api.id = list.Id
	return getMembers(client, &api, "data.list.members_timeline.timeline.instructions")
}

func (list *List) GetId() int64 {
	return int64(list.Id)
}

func (list *List) Title() string {
	return fmt.Sprintf("%s(%d)", list.Name, list.Id)
}

type UserFollowing struct {
	creator *User
}

func (fo UserFollowing) GetMembers(client *resty.Client) ([]*User, error) {
	api := following{}
	api.count = 200
	api.uid = fo.creator.Id
	return getMembers(client, &api, "data.user.result.timeline.timeline.instructions")
}

func (fo UserFollowing) GetId() int64 {
	return -int64(fo.creator.Id)
}

func (fo UserFollowing) Title() string {
	name := fmt.Sprintf("%s's Following", fo.creator.ScreenName)
	return name
}
