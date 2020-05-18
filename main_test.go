package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/api/sheets/v4"

	"cloud.google.com/go/datastore"
	"github.com/khipkin/geddit"
)

type fakeRedditSession struct {
	numComments int
	submittions []*geddit.Submission
}

func (frs *fakeRedditSession) LoginAuth(username, password string) error { return nil }
func (frs *fakeRedditSession) Reply(r geddit.Replier, comment string) (*geddit.Comment, error) {
	frs.numComments++
	return &geddit.Comment{FullID: "uniqueComment"}, nil
}
func (frs *fakeRedditSession) SubredditSubmissions(subreddit string, sort geddit.PopularitySort, params geddit.ListingOptions) ([]*geddit.Submission, error) {
	return frs.submittions, nil
}
func (frs *fakeRedditSession) Throttle(interval time.Duration) {}
func (frs *fakeRedditSession) Comment(fullID string) (*geddit.Comment, error) {
	return &geddit.Comment{FullID: fullID}, nil
}

type fakeDatastoreClient struct {
	lastPut map[string]map[string]interface{}
}

func (fdc *fakeDatastoreClient) Get(ctx context.Context, key *datastore.Key, dst interface{}) error {
	src, ok := fdc.lastPut[key.Kind][key.Name]
	if !ok {
		return datastore.ErrNoSuchEntity
	}
	if dstPt, ok := dst.(*PageToken); ok {
		srcPt := src.(*PageToken)
		dstPt.LastProcessedUser = srcPt.LastProcessedUser
		return nil
	}
	return nil
}
func (fdc *fakeDatastoreClient) GetAll(ctx context.Context, q *datastore.Query, dst interface{}) (keys []*datastore.Key, err error) {
	vals := fdc.lastPut["PageToken"]
	k := []*datastore.Key{}
	dstPts := dst.(*[]*PageToken)
	for id, val := range vals {
		*dstPts = append(*dstPts, val.(*PageToken))
		k = append(k, datastore.NameKey("PageToken", id, nil))
	}
	return k, nil
}
func (fdc *fakeDatastoreClient) Put(ctx context.Context, key *datastore.Key, src interface{}) (*datastore.Key, error) {
	fdc.lastPut[key.Kind][key.Name] = src
	return key, nil
}
func (fdc *fakeDatastoreClient) Delete(ctx context.Context, key *datastore.Key) error {
	delete(fdc.lastPut[key.Kind], key.Name)
	return nil
}

func fakeSummoner(redditSubmissions []*geddit.Submission, spreadsheetValues *sheets.ValueRange) *summoner {
	return &summoner{
		redditSession: &fakeRedditSession{submittions: redditSubmissions},
		datastoreClient: &fakeDatastoreClient{lastPut: map[string]map[string]interface{}{
			"Entity":    map[string]interface{}{},
			"PageToken": map[string]interface{}{},
		}},
		readSpreadsheetValuesFunc: func(string, string) (*sheets.ValueRange, error) { return spreadsheetValues, nil },
	}
}

func fakeUserName(num int) string {
	return fmt.Sprintf("u/user-%d", num)
}

func generateFakeUsers(numUsers int) [][]interface{} {
	values := make([][]interface{}, numUsers)
	for i := 0; i < numUsers; i++ {
		v := make([]interface{}, 1)
		v[0] = fakeUserName(i)
		values[i] = v
	}
	return values
}

func TestBuildSummonStringsNoValues(t *testing.T) {
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: [][]interface{}{}})
	summons, lastUser, err := s.buildSummonStrings("" /*lastProcessedUser*/)
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 0 {
		t.Fatalf("buildSummonStrings returned non-empty results: %s", summons)
	}
	if lastUser != "" {
		t.Fatalf("buildSummonStrings returned non-blank last user for fewer than max results: %s", lastUser)
	}
}

func TestBuildSummonStringsSomeValuesWithRemainder(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	summons, lastUser, err := s.buildSummonStrings("" /*lastProcessedUser*/)
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 2 {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
	if lastUser != "" {
		t.Fatalf("buildSummonStrings returned non-blank last user for fewer than max results: %s", lastUser)
	}
}

func TestBuildSummonStringsSomeValuesNoRemainder(t *testing.T) {
	const numUsers = maxRedditTagsPerComment
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	summons, lastUser, err := s.buildSummonStrings("" /*lastProcessedUser*/)
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 1 {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
	if lastUser != "" {
		t.Fatalf("buildSummonStrings returned non-blank last user for fewer than max results: %s", lastUser)
	}
}

func TestBuildSummonStringsMoreThanMaxValues(t *testing.T) {
	const numUsers = maxUsersPerSession + 1
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	summons, lastUser, err := s.buildSummonStrings("" /*lastProcessedUser*/)
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != maxUsersPerSession/maxRedditTagsPerComment {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
	if lastUser == "" {
		t.Fatalf("buildSummonStrings returned blank last user for more than max results: %s", lastUser)
	}

	summons, lastUser, err = s.buildSummonStrings(lastUser)
	if err != nil {
		t.Fatalf("buildSummonStrings call failed: %v", err)
	}
	if len(summons) != 1 {
		t.Fatalf("buildSummonStrings returned results of wrong length: %s", summons)
	}
	if lastUser != "" {
		t.Fatalf("buildSummonStrings returned blank last user for more than max results: %s", lastUser)
	}
}

func TestSummonContestantsNoUsers(t *testing.T) {
	post := &geddit.Submission{FullID: "t3_12345"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: [][]interface{}{}})

	if err := s.summonContestants(context.Background(), post, nil /*PageToken*/); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestSummonContestantsSomeUsers(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 3 // main comment, 2 child comments
	post := &geddit.Submission{FullID: "t3_12345"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.summonContestants(context.Background(), post, nil /*PageToken*/); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestSummonContestantsMoreThanMaxUsers(t *testing.T) {
	const numUsers = maxUsersPerSession*2 + 1
	expectedNumComments := maxUsersPerSession/maxRedditTagsPerComment + 1 // main comment, maxUsersPerSession/maxRedditTagsPerComment child comments
	ctx := context.Background()
	post := &geddit.Submission{FullID: "t3_12345"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	// Summon the first batch of contestants.
	if err := s.summonContestants(ctx, post, nil /*PageToken*/); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
	// Make sure correct page token was written to Datastore.
	pt := &PageToken{}
	if err := s.datastoreClient.Get(ctx, datastore.NameKey("PageToken", post.FullID, nil), pt); err != nil {
		t.Fatalf("failed to fetch pagetoken from datastore: %v", err)
	}
	expectedLastProcessedUser := fakeUserName(maxUsersPerSession - 1)
	if pt.LastProcessedUser != expectedLastProcessedUser {
		t.Fatalf("summonContestants wrote pagetoken with wrong LastProcessedUser (got: %s, want: %s)", pt.LastProcessedUser, expectedLastProcessedUser)
	}

	// Summon the second batch of contestants.
	if err := s.summonContestants(ctx, post, pt); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	expectedLastProcessedUser = fakeUserName(maxUsersPerSession*2 - 1)
	expectedNumComments += maxUsersPerSession / maxRedditTagsPerComment
	if fsr.numComments != expectedNumComments {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
	// Make sure correct page token was written to Datastore.
	if err := s.datastoreClient.Get(ctx, datastore.NameKey("PageToken", post.FullID, nil), pt); err != nil {
		t.Fatalf("failed to fetch pagetoken from datastore: %v", err)
	}
	if pt.LastProcessedUser != expectedLastProcessedUser {
		t.Fatalf("summonContestants wrote pagetoken with wrong LastProcessedUser (got: %s, want: %s)", pt.LastProcessedUser, expectedLastProcessedUser)
	}

	// Summon the third (last) batch of contestants.
	if err := s.summonContestants(ctx, post, pt); err != nil {
		t.Fatalf("summonContestants call failed: %v", err)
	}

	expectedLastProcessedUser = fakeUserName(maxUsersPerSession * 2)
	expectedNumComments = expectedNumComments + 1
	if fsr.numComments != expectedNumComments {
		t.Fatalf("summonContestants made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
	// Make page token was deleted from Datastore.
	if err := s.datastoreClient.Get(ctx, datastore.NameKey("PageToken", post.FullID, nil), pt); err != datastore.ErrNoSuchEntity {
		t.Fatalf("Datastore did not return expected ErrNoSuchEntity: %v", err)
	}
}

func TestHandlePossibleCompetitionPostIgnoresWinnersPost(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition winners - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.handlePossibleCompetitionPost(context.Background(), post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestHandlePossibleCompetitionPostNotHandledYet(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 3 // main comment, 2 child comments
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.handlePossibleCompetitionPost(context.Background(), post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestHandlePossibleCompetitionPostInProgress(t *testing.T) {
	const numUsers = maxUsersPerSession + 1
	ctx := context.Background()
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	val := PageToken{
		MainCommentFullID: "t1_6789",
		LastProcessedUser: fakeUserName(maxUsersPerSession - 1),
	}
	if _, err := s.datastoreClient.Put(ctx, datastore.NameKey("PageToken", post.FullID, nil), &val); err != nil {
		t.Fatalf("test failed to setup datastore state: %v", err)
	}

	if err := s.handlePossibleCompetitionPost(ctx, post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 1 {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 1)
	}
	// Make page token was deleted from Datastore.
	if err := s.datastoreClient.Get(ctx, datastore.NameKey("PageToken", post.FullID, nil), &val); err != datastore.ErrNoSuchEntity {
		t.Fatalf("Datastore did not return expected ErrNoSuchEntity: %v", err)
	}
}

func TestHandlePossibleCompetitionPostAlreadyHandled(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	ctx := context.Background()
	post := &geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"}
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	val := struct{}{}
	if _, err := s.datastoreClient.Put(ctx, datastore.NameKey("Entity", post.FullID, nil), &val); err != nil {
		t.Fatalf("test failed to setup datastore state: %v", err)
	}

	if err := s.handlePossibleCompetitionPost(ctx, post); err != nil {
		t.Fatalf("handlePossibleCompetitionPost call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 0 {
		t.Fatalf("handlePossibleCompetitionPost made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 0)
	}
}

func TestCheckPostsHandlesSeveralPosts(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 6 // 2 * (main comment, 2 child comments)
	submissions := []*geddit.Submission{
		&geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"},
		&geddit.Submission{FullID: "t3_67890", Title: "[MOD] February's competition - more text"},
	}
	s := fakeSummoner(submissions, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.checkPosts(context.Background()); err != nil {
		t.Fatalf("checkPosts call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("checkPosts made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestCheckPostsIgnoresDuplicatePosts(t *testing.T) {
	const numUsers = maxRedditTagsPerComment + 1
	const expectedNumComments = 3 // main comment, 2 child comments
	submissions := []*geddit.Submission{
		&geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"},
		&geddit.Submission{FullID: "t3_12345", Title: "[MOD] January's competition - more text"},
	}
	s := fakeSummoner(submissions, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})

	if err := s.checkPosts(context.Background()); err != nil {
		t.Fatalf("checkPosts call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != expectedNumComments {
		t.Fatalf("checkPosts made unexpected number of comments (got: %d, want: %d)", fsr.numComments, expectedNumComments)
	}
}

func TestCheckPostsHandlesPageToken(t *testing.T) {
	const (
		numUsers = maxRedditTagsPerComment + 1

		commentID = "t1_12345"
		postID    = "t3_12345"
	)
	s := fakeSummoner(nil /*redditSubmissions*/, &sheets.ValueRange{Values: generateFakeUsers(numUsers)})
	pt := &PageToken{MainCommentFullID: commentID, LastProcessedUser: fakeUserName(numUsers - maxRedditTagsPerComment)}
	if _, err := s.datastoreClient.Put(context.Background(), datastore.NameKey("PageToken", postID, nil), pt); err != nil {
		t.Fatalf("failed to set up Datastore state: %v", err)
	}

	if err := s.checkPosts(context.Background()); err != nil {
		t.Fatalf("checkPosts call failed: %v", err)
	}

	var fsr *fakeRedditSession
	fsr = s.redditSession.(*fakeRedditSession)
	if fsr.numComments != 1 {
		t.Fatalf("checkPosts made unexpected number of comments (got: %d, want: %d)", fsr.numComments, 1)
	}
}
